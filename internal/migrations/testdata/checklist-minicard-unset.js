  {
    // checklist-minicard-unset: `showChecklistAtMinicard: false` written by the
    // old schema default, which now means something it did not mean then.
    //
    // The minicard used to ask `board.allowsChecklistsOnMinicard ||
    // checklist.showChecklistAtMinicard`, and the board flag is on by default - so
    // a checklist's own false meant "follow the board" and nothing else. It is an
    // OVERRIDE now (models/lib/minicardChecklistVisibility.js), so a stored false
    // means "hide this one", and leaving the ones the old default wrote in place
    // would hide every checklist on every minicard - the mirror image of the bug
    // being fixed. Unsetting them restores exactly the old meaning: no opinion,
    // follow the board.
    //
    // ONCE EVER, not once per version - every other step here may re-run on the
    // next release because re-running changes nothing. This one is different: after
    // it has run, a stored false is a CHOICE somebody made with the toggle, and
    // clearing those on the next upgrade would quietly put their checklists back on
    // the minicard. So it records itself in the marker collection and never looks
    // at the data again.
    name: 'checklist-minicard-unset',
    async check(db) {
      const done = await db.collection(MARKER_COLL)
        .findOne({ _id: CHECKLIST_MINICARD_MARKER_ID }, { projection: { _id: 1 } });
      if (done) return false;
      return !!await db.collection('checklists').findOne(
        { showChecklistAtMinicard: false },
        { projection: { _id: 1 } },
      );
    },
    async run(db) {
      const r = await db.collection('checklists').updateMany(
        { showChecklistAtMinicard: false },
        { $unset: { showChecklistAtMinicard: '' } },
      );
      await db.collection(MARKER_COLL).updateOne(
        { _id: CHECKLIST_MINICARD_MARKER_ID },
        { $set: { at: new Date() } },
        { upsert: true },
      );
      return { fixed: (r && r.modifiedCount) || 0, unresolved: 0 };
    },
  },

  {
