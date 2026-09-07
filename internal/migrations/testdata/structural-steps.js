// Snapshot from WeKan server/lib/schemaUpgradeSteps.js.
// Structural migration parity reference; no Meteor dependencies.
const crypto = require("crypto");
const ID_CHARS = '23456789ABCDEFGHJKLMNPQRSTWXYZabcdefghijkmnopqrstuvwxyz';
function randomId(len = 17) {
  // Rejection sampling: 256 is not a multiple of ID_CHARS.length (55), so a
  // plain `byte % 55` skews toward the first characters (CodeQL
  // js/biased-cryptographic-random). Accept only bytes below the largest
  // multiple of the alphabet size and resample the rest.
  const limit = 256 - (256 % ID_CHARS.length);
  let out = '';
  while (out.length < len) {
    const bytes = crypto.randomBytes(len - out.length);
    for (let i = 0; i < bytes.length && out.length < len; i++) {
      if (bytes[i] < limit) out += ID_CHARS[bytes[i] % ID_CHARS.length];
    }
  }
  return out;
}

const MISSING_OR_EMPTY = { $in: [null, ''] };
const steps = [
  {
    // add-swimlanes (v0.65) + the dangling-listId rescue that the disabled
    // repair tools used to provide, PLUS the Swimlanes-view visibility rescue
    // of #1959/#1971: every board gets a swimlane, every list/card gets a
    // swimlaneId, cards whose listId points nowhere are rescued to a visible
    // list, and unarchived cards whose swimlaneId points at a DELETED,
    // ARCHIVED or other-board swimlane (invisible in Swimlanes view) are
    // reassigned to the board's first visible swimlane — so all lists and
    // cards are visible in both the Swimlanes view and the Lists view.
    name: 'swimlane-structure',
    async check(db) {
      // bounded existence probes — no scans. NOTE: lists with EMPTY swimlaneId
      // are deliberately NOT backfilled — a shared board-wide list ('') renders
      // in every swimlane via myLists()'s null/'' fallback, and stamping it to
      // one swimlane would HIDE it from all other swimlanes in Swimlanes view.
      if (await db.collection('cards').findOne({ swimlaneId: MISSING_OR_EMPTY }, { projection: { _id: 1 } })) return true;
      // set-joins via distinct(): result sizes are bounded by the number of
      // lists/swimlanes/boards, never by the number of cards
      const boardIds = (await db.collection('boards').distinct('_id')).map(String);
      const swimlaneBoards = new Set((await db.collection('swimlanes').distinct('boardId')).map(String));
      if (boardIds.some(b => !swimlaneBoards.has(b))) return true;
      const listIds = new Set((await db.collection('lists').distinct('_id')).map(String));
      const usedListIds = (await db.collection('cards').distinct('listId')).map(String);
      if (usedListIds.some(id => id && id !== 'null' && id !== 'undefined' && !listIds.has(id))) return true;
      // #1959/#1971: any unarchived card under a DELETED or foreign swimlane?
      //
      // Not an archived one, and that is a correction rather than a detail.
      // Archiving a swimlane leaves its cards where they are, `archived: false`,
      // hidden because their swimlane is hidden - so treating "swimlane is
      // archived" as breakage hoisted them into the first visible swimlane and
      // they came back, sometimes years later. Reported by email on 2026-08-13:
      // "Previously archived cards (some several years old) have reappeared.
      // These cards have incorrectly been placed in the top swimlane."
      //
      // A card is only orphaned when its swimlane does not exist at all, or
      // belongs to another board. existsById knows the difference.
      const swimlanes = await loadVisibleSwimlanes(db);
      const usedSwimlaneIds = (await db.collection('cards').distinct('swimlaneId', { archived: false })).map(String);
      for (const swId of usedSwimlaneIds) {
        if (!swId || swId === 'null' || swId === 'undefined') continue;
        const owner = swimlanes.existsById.get(swId);
        if (!owner) return true;   // deleted swimlane: the card has nowhere to be
        // same-board check needs the referencing boards (bounded by board count)
        const boards = (await db.collection('cards').distinct('boardId', { swimlaneId: swId, archived: false })).map(String);
        if (boards.some(b => b !== owner)) return true;   // foreign swimlane
      }
      return false;
    },
    async run(db) {
      let fixed = 0;
      const now = new Date();
      // a VISIBLE (unarchived, non-template) default swimlane per board —
      // boards and swimlanes are few. #1959: assigning to any first swimlane
      // could pick an archived one, and the card stayed invisible.
      const boards = await db.collection('boards').find({}, { projection: { _id: 1 } }).toArray();
      let visible = await loadVisibleSwimlanes(db);
      const defaultSwimlaneOf = new Map(visible.firstOfBoard);
      for (const b of boards) {
        const boardId = String(b._id);
        if (!defaultSwimlaneOf.has(boardId)) {
          const sw = { _id: randomId(), title: 'Default', boardId, archived: false, sort: 0, type: 'swimlane', createdAt: now, modifiedAt: now, updatedAt: now };
          await db.collection('swimlanes').insertOne(sw);
          defaultSwimlaneOf.set(boardId, String(sw._id));
          visible.byId.set(String(sw._id), boardId);
          fixed++;
        }
      }
      // missing swimlaneId on CARDS: ONE server-side updateMany per board — no
      // per-document round trips, no matter how many thousands of cards.
      // Lists are NOT stamped (see check() note: '' means shared across swimlanes).
      for (const [boardId, swId] of defaultSwimlaneOf) {
        const r = await db.collection('cards').updateMany(
          { boardId, swimlaneId: MISSING_OR_EMPTY },
          { $set: { swimlaneId: swId } },
        );
        fixed += (r && r.modifiedCount) || 0;
      }
      // dangling listId rescue: distinct() finds the bad ids (bounded by the
      // number of lists ever referenced), then one updateMany per (bad id, board)
      const lists = await db.collection('lists').find({}, { projection: { _id: 1, boardId: 1, swimlaneId: 1, sort: 1 } }).toArray();
      const listIds = new Set(lists.map(l => String(l._id)));
      const firstListOf = new Map();
      for (const l of lists.sort((a, b2) => (a.sort || 0) - (b2.sort || 0))) {
        if (!firstListOf.has(String(l.boardId))) firstListOf.set(String(l.boardId), l);
      }
      const rescuedListOf = new Map();
      const badListIds = (await db.collection('cards').distinct('listId'))
        .map(String).filter(id => id && id !== 'null' && id !== 'undefined' && !listIds.has(id));
      for (const badId of badListIds) {
        const boardsOfBad = (await db.collection('cards').distinct('boardId', { listId: badId })).map(String);
        for (const boardId of boardsOfBad) {
          let target = firstListOf.get(boardId) || rescuedListOf.get(boardId);
          if (!target) {
            target = { _id: randomId(), title: 'Rescued Data', boardId, swimlaneId: defaultSwimlaneOf.get(boardId) || '', archived: false, sort: 0, type: 'list', createdAt: now, modifiedAt: now, updatedAt: now };
            await db.collection('lists').insertOne(target);
            rescuedListOf.set(boardId, target);
          }
          const r = await db.collection('cards').updateMany(
            { boardId, listId: badId },
            { $set: { listId: String(target._id), swimlaneId: String(target.swimlaneId || defaultSwimlaneOf.get(boardId) || '') } },
          );
          fixed += (r && r.modifiedCount) || 0;
        }
      }
      // #1959/#1971: unarchived cards whose swimlaneId points at a DELETED or
      // other-board swimlane never render in the Swimlanes view. A card under an
      // ARCHIVED swimlane is NOT one of those: it is hidden on purpose, and
      // moving it to the first visible swimlane is how archived cards reappeared
      // years later (email, 2026-08-13). distinct() bounds the work by the
      // number of referenced swimlanes; the fix is one updateMany per (bad
      // swimlane, board).
      const usedSwimlaneIds = (await db.collection('cards').distinct('swimlaneId', { archived: false })).map(String);
      for (const swId of usedSwimlaneIds) {
        if (!swId || swId === 'null' || swId === 'undefined') continue;
        const owner = visible.existsById.get(swId);
        const boardsOfSw = (await db.collection('cards').distinct('boardId', { swimlaneId: swId, archived: false })).map(String);
        for (const boardId of boardsOfSw) {
          if (owner === boardId) continue;   // visible swimlane on the right board
          const target = defaultSwimlaneOf.get(boardId);
          if (!target || target === swId) continue;
          const r = await db.collection('cards').updateMany(
            { boardId, swimlaneId: swId, archived: false },
            { $set: { swimlaneId: target } },
          );
          fixed += (r && r.modifiedCount) || 0;
        }
      }
      return { fixed, unresolved: 0 };
    },
  },

  {
    // v7.98 'per-swimlane lists' era: the migrate-lists-to-per-swimlane step
    // stamped every list to the default swimlane (hiding it from every other
    // swimlane in today's Swimlanes view), and the v8.07–v8.19 board-open
    // repair DUPLICATED lists per swimlane (rendering duplicate columns per
    // title in today's board-wide Lists view). Current WeKan renders shared
    // lists (swimlaneId '') in every swimlane, so: merge same-title copies
    // back into ONE shared list — cards keep their own swimlaneId, preserving
    // per-swimlane grouping and order, and card/list drag between swimlanes
    // keeps working — then clear the stamped swimlaneIds. Copies the user
    // RENAMED differently per swimlane are deliberately kept as their own
    // lists (merging them would lose the distinct names). Boards are detected
    // by the era's own markers (fixMissingListsCompleted /
    // comprehensiveMigrationCompleted), duplicate titles, or a list whose
    // cards disagree with its stamped swimlane; healthy boards are untouched.
    name: 'merge-per-swimlane-lists',
    async check(db) {
      if (await db.collection('boards').findOne(
        { $or: [{ fixMissingListsCompleted: true }, { comprehensiveMigrationCompleted: true }] },
        { projection: { _id: 1 } },
      )) return true;
      const boardIds = (await db.collection('lists').distinct('boardId', { swimlaneId: { $nin: [null, ''] }, type: { $ne: 'template-list' } })).map(String);
      for (const boardId of boardIds) {
        const lists = await db.collection('lists').find(
          { boardId, type: { $ne: 'template-list' } },
          { projection: { title: 1, swimlaneId: 1, archived: 1 } },
        ).toArray();
        const seen = new Set();
        for (const l of lists) {
          if (l.archived === true) continue;
          if (seen.has(l.title)) return true;   // per-swimlane duplicate copies
          seen.add(l.title);
        }
        for (const l of lists) {
          if (!l.swimlaneId) continue;
          // Shape A (v7.98 stamp): cards of another swimlane point at this list
          if (await db.collection('cards').findOne(
            { listId: String(l._id), swimlaneId: { $nin: [String(l.swimlaneId), null, ''] } },
            { projection: { _id: 1 } },
          )) return true;
        }
      }
      return false;
    },
    async run(db) {
      let fixed = 0;
      const markerBoards = (await db.collection('boards').find(
        { $or: [{ fixMissingListsCompleted: true }, { comprehensiveMigrationCompleted: true }] },
        { projection: { _id: 1 } },
      ).toArray()).map(b => String(b._id));
      const stampedBoards = (await db.collection('lists').distinct('boardId', { swimlaneId: { $nin: [null, ''] }, type: { $ne: 'template-list' } })).map(String);
      for (const boardId of new Set([...markerBoards, ...stampedBoards])) {
        const lists = await db.collection('lists').find(
          { boardId, type: { $ne: 'template-list' } },
          { projection: { title: 1, swimlaneId: 1, archived: 1, sort: 1, createdAt: 1 } },
        ).toArray();
        // Only act on boards that actually show an era symptom (duplicate titles
        // or a card/list swimlane mismatch or the marker) — a healthy natively
        // per-swimlane board is left exactly as it is.
        const unarchived = lists.filter(l => l.archived !== true);
        const titleCount = new Map();
        for (const l of unarchived) titleCount.set(l.title, (titleCount.get(l.title) || 0) + 1);
        let mismatch = false;
        if (!markerBoards.includes(boardId) && ![...titleCount.values()].some(n => n > 1)) {
          for (const l of unarchived) {
            if (!l.swimlaneId) continue;
            if (await db.collection('cards').findOne(
              { listId: String(l._id), swimlaneId: { $nin: [String(l.swimlaneId), null, ''] } },
              { projection: { _id: 1 } },
            )) { mismatch = true; break; }
          }
          if (!mismatch) continue;
        }
        // merge same-title unarchived copies into one canonical shared list
        const groups = new Map();
        for (const l of unarchived) {
          const g = groups.get(l.title) || [];
          g.push(l);
          groups.set(l.title, g);
        }
        for (const group of groups.values()) {
          if (group.length < 2) continue;
          group.sort((a, b) =>
            ((a.swimlaneId ? 1 : 0) - (b.swimlaneId ? 1 : 0)) ||
            ((a.createdAt || 0) - (b.createdAt || 0)) ||
            ((a.sort || 0) - (b.sort || 0)));
          const canonical = group[0];
          for (const m of group.slice(1)) {
            // cards KEEP their swimlaneId — only the column pointer moves
            await db.collection('cards').updateMany({ listId: String(m._id) }, { $set: { listId: String(canonical._id) } });
            try { await db.collection('activities').updateMany({ listId: String(m._id) }, { $set: { listId: String(canonical._id) } }); } catch { /* history relink is best-effort */ }
            await db.collection('lists').deleteOne({ _id: m._id });
            fixed++;
          }
        }
        // shared lists render in EVERY swimlane via the '' fallback
        const r = await db.collection('lists').updateMany(
          { boardId, type: { $ne: 'template-list' }, swimlaneId: { $nin: [null, ''] } },
          { $set: { swimlaneId: '' } },
        );
        fixed += (r && r.modifiedCount) || 0;
        // drop the era markers so the on-demand repair tools are usable again
        await db.collection('boards').updateOne(
          { _id: boardId },
          { $unset: { fixMissingListsCompleted: 1, fixMissingListsCompletedAt: 1, comprehensiveMigrationCompleted: 1 } },
        );
      }
      return { fixed, unresolved: 0 };
    },
  },
];
async function loadVisibleSwimlanes(db) {
  const byId = new Map();
  const firstOfBoard = new Map();
  // existsById: EVERY swimlane, archived ones included. An archived swimlane is
  // not a missing one - it is a swimlane whose cards are meant to be out of
  // sight - and the difference is the whole of the report below.
  const existsById = new Map();
  const all = await db.collection('swimlanes')
    .find({}, { projection: { _id: 1, boardId: 1, archived: 1, type: 1, sort: 1 } }).toArray();
  for (const s of all.sort((a, b) => (a.sort || 0) - (b.sort || 0))) {
    existsById.set(String(s._id), String(s.boardId));
    if (s.archived === true || s.type === 'template-swimlane') continue;
    byId.set(String(s._id), String(s.boardId));
    if (!firstOfBoard.has(String(s.boardId))) firstOfBoard.set(String(s.boardId), String(s._id));
  }
  return { byId, firstOfBoard, existsById };
}

module.exports = { steps };
