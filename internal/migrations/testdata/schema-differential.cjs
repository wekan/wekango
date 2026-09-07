'use strict';

// Execute the actual parent WeKan source against a real embedded database.
// Only Meteor's absolute /models module alias is adapted; no migration or
// driver calls are replaced, and no expected outcome is copied from Go.
const path = require('node:path');
const Module = require('node:module');
const sourceRoot = process.env.WEKAN_SOURCE_ROOT;
if (!sourceRoot) throw new Error('WEKAN_SOURCE_ROOT is required');
const parentRequire = Module.createRequire(path.join(sourceRoot, 'package.json'));
// CI may install only the source driver's pinned npm package, independently
// of the full Meteor application dependencies.
const moduleOverride = process.env.WEKAN_MONGODB_MODULE;
if (moduleOverride && !path.isAbsolute(moduleOverride)) throw new Error('WEKAN_MONGODB_MODULE must be an absolute module path');
const { MongoClient } = moduleOverride ? require(moduleOverride) : parentRequire('mongodb');
const originalResolve = Module._resolveFilename;
Module._resolveFilename = function(request, ...args) {
  if (request.startsWith('/models/')) request = path.join(sourceRoot, request.slice(1));
  return originalResolve.call(this, request, ...args);
};
const { runSchemaUpgrade, getUpgradeState } = parentRequire('./server/lib/schemaUpgradeSteps.js');
const fixtureDate = new Date('2001-02-03T04:05:06.000Z');
const collections = ['boards', 'swimlanes', 'lists', 'cards', 'activities',
  'checklists', 'checklistItems', 'customFields', 'attachments', 'avatars',
  'settings', '_wekan_migration'];
const fixture = {
  boards: [
    { _id: 'board-old', title: 'Old board', permission: 'PUBLIC', allowsComments: false,
      fixMissingListsCompleted: true, fixMissingListsCompletedAt: fixtureDate,
      members: [{ userId: 'old-user' }, { userId: 'inactive', isActive: false }], createdAt: fixtureDate },
    { _id: 'board-other', archived: true, permission: 'private', members: [] },
  ],
  swimlanes: [
    { _id: 'lane-visible', boardId: 'board-old', sort: 1 },
    { _id: 'lane-archived', boardId: 'board-old', sort: -1, archived: true },
    { _id: 'lane-other', boardId: 'board-other', sort: 0, archived: false },
  ],
  lists: [
    { _id: 'list-shared', boardId: 'board-old', title: 'Todo', swimlaneId: '', sort: 0, createdAt: fixtureDate },
    { _id: 'list-copy', boardId: 'board-old', title: 'Todo', swimlaneId: 'lane-visible', sort: 1 },
    { _id: 'list-renamed', boardId: 'board-old', title: 'Renamed', swimlaneId: 'lane-visible', archived: true },
    { _id: 'list-template', boardId: 'board-old', title: 'Todo', swimlaneId: 'lane-visible', type: 'template-list', archived: false },
  ],
  cards: [
    { _id: 'card-nan', boardId: 'board-old', listId: 'list-shared', swimlaneId: 'lane-visible', archived: false, sort: NaN },
    { _id: 'card-old', boardId: 'board-old', listId: 'list-copy', sort: -3, createdAt: fixtureDate },
    { _id: 'card-orphan', boardId: 'board-old', listId: 'deleted-list', swimlaneId: 'deleted-lane', archived: false },
    { _id: 'card-foreign', boardId: 'board-old', listId: 'list-shared', swimlaneId: 'lane-other', archived: false },
    { _id: 'card-hidden', boardId: 'board-old', listId: 'list-shared', swimlaneId: 'lane-archived', archived: false },
  ],
  activities: [{ _id: 'activity-old', listId: 'list-copy', cardId: 'card-old', createdAt: fixtureDate }],
  checklists: [
    { _id: 'checklist-old', cardId: 'card-old', showChecklistAtMinicard: false,
      items: [{ title: 'Second', sort: 2, isFinished: false }, { title: 'First', sort: 0, isFinished: true }, { title: '', sort: 3 }] },
    { _id: 'checklist-explicit', cardId: 'card-old', showChecklistAtMinicard: true },
    { _id: 'checklist-empty', cardId: 'card-old', items: [] },
  ],
  checklistItems: [{ _id: 'already-extracted', checklistId: 'checklist-old', cardId: 'card-old', title: 'First', sort: 0, isFinished: true, createdAt: fixtureDate }],
  customFields: [
    { _id: 'field-old', name: 'Old field', boardId: 'board-old' },
    { _id: 'field-preserved', name: 'Shared field', boardId: 'board-old', boardIds: ['board-other'], settings: { options: ['Keep'] } },
  ],
  attachments: [{ _id: 'attachment-image', name: 'legacy.PNG', meta: { cardId: 'card-old' } },
    { _id: 'attachment-text', name: 'notes.txt', type: 'text/plain' }],
  settings: [{ _id: 'settings', productName: '  Differential WeKan  ' }],
  _wekan_migration: [{ _id: 'schema-upgrade', lastCheck: { version: 'old-version', at: fixtureDate }, keep: 'history' }],
};

function canonical(value, generatedIds, key = '', location = '') {
  if (value instanceof Date) {
    if (value.toISOString() === fixtureDate.toISOString()) return value.toISOString();
    if (['at', 'doneAt', 'createdAt', 'modifiedAt', 'updatedAt', 'startedAt', 'finishedAt'].includes(key)) return '<generated-date>';
    return value.toISOString();
  }
  if (typeof value === 'string') {
    if (generatedIds.has(value)) return generatedIds.get(value);
    if (['at', 'doneAt', 'startedAt', 'finishedAt'].includes(key) && /^\d{4}-\d{2}-\d{2}T/.test(value)) {
      if (value === fixtureDate.toISOString()) return value;
      if (Number.isNaN(Date.parse(value))) throw new Error(`invalid date ${location}`);
      return '<generated-date>';
    }
    return value;
  }
  if (Array.isArray(value)) return value.map((v, i) => canonical(v, generatedIds, '', `${location}[${i}]`));
  if (value && typeof value === 'object') return Object.fromEntries(Object.keys(value).sort().map(k => [k, canonical(value[k], generatedIds, k, `${location}.${k}`)]));
  return value;
}
async function dump(db) {
  const documents = {};
  for (const c of collections) documents[c] = await db.collection(c).find({}).toArray();
  const generatedIds = new Map();
  for (const d of documents.checklistItems) {
    if (d._id === 'already-extracted') continue;
    if (!/^[23456789ABCDEFGHJKLMNPQRSTWXYZabcdefghijkmnopqrstuvwxyz]{17}$/.test(d._id)) throw new Error('invalid generated Meteor ID');
    generatedIds.set(d._id, `<item:${d.checklistId}:${d.sort}:${d.title}>`);
    if (!(d.createdAt instanceof Date) || !(d.modifiedAt instanceof Date) || +d.createdAt !== +d.modifiedAt) throw new Error('generated item timestamps differ or lost BSON date type');
  }
  const out = canonical(documents, generatedIds);
  for (const c of collections) out[c].sort((a, b) => String(a._id).localeCompare(String(b._id)));
  return out;
}
async function input() { let text = ''; for await (const chunk of process.stdin) text += chunk; return text ? JSON.parse(text) : null; }
(async () => {
  const [mode, uri, dbName, ...rest] = process.argv.slice(2);
  const client = new MongoClient(uri, { serverSelectionTimeoutMS: 5000 });
  await client.connect();
  try {
    if (mode === 'seed') {
      for (const name of [dbName, rest[0]]) {
        const db = client.db(name);
        for (const [c, docs] of Object.entries(fixture)) await db.collection(c).insertMany(docs.map(d => ({ ...d })));
        if (rest[1] === 'unknown-kind') await db.collection('attachments').insertOne({ _id: 'unknown-kind' });
        if (rest[1] === 'unresolved-filesystem') await db.collection('attachments').insertOne({ _id: 'missing-file', name: 'source-differential-missing.bin', isImage: false, isVideo: false, versions: { original: { storage: 'fs', path: path.join(rest[2], 'nonexistent-source-directory', 'source-differential-missing.bin') } } });
      }
      process.stdout.write('{}');
      return;
    }
    const db = client.db(dbName);
    let report;
    if (mode === 'source') {
      report = [];
      for (const options of [{ appVersion: 'differential-v1' }, { appVersion: 'differential-v1' },
        { appVersion: 'differential-v1', force: true }, { appVersion: 'differential-v2' }]) {
        const result = await runSchemaUpgrade(db, { ...options, writablePath: rest[0] });
        const state = JSON.parse(JSON.stringify(getUpgradeState()));
        report.push({ result, state, documents: await dump(db) });
      }
    } else if (mode === 'dump') {
      const supplied = await input();
      report = { ...supplied, documents: await dump(db) };
    } else throw new Error(`unknown mode ${mode}`);
    process.stdout.write(JSON.stringify(canonical(report, new Map())));
  } finally { await client.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
