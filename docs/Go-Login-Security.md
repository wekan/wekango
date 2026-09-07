# Login security review

The saved CodeQL alerts in the September 7 review point to the local-password
login's account lookup (`go/sql-injection`) and SHA-256 preprocessing
(`go/weak-sensitive-data-hashing`). Both paths already rejected the reported
attack forms. The implementation now makes their boundaries explicit and tests
them directly.

## Account lookup

Login accepts a string username or a string email, never an operator object or
array. Two fixed BSON struct types provide the only query keys: `username` and
`emails.address`. Their values serialize as BSON strings, including quotes,
Unicode and text resembling JSON operators. No request object, raw BSON,
interpolated JSON or query-language string is passed to the database.

The comment at the lookup documents this typed BSON boundary.
Tests inspect the actual BSON encoding and send JSON and form operator attempts
to a real embedded database. They verify that no session is issued and that
legitimate literal names containing metacharacters still work.

## Existing Meteor passwords

Meteor stores salted bcrypt verifiers over the hexadecimal SHA-256 prehash of
the password. Removing that prehash would break existing accounts. SHA-256 by
itself is not the stored password verifier: `compareMeteorPassword` always
checks the expensive bcrypt verifier and returns only a boolean.

Tests prove
that distinct salts work, changed suffixes beyond 72 bytes are rejected, and
plaintext, raw SHA-256, hexadecimal SHA-256 and malformed hashes cannot serve
as password verifiers. Presenting the prehash as the password also fails.

## Follow-up alert #3

The saved `.tools/wekangosec2` report flags the prehash in
`internal/api/login_security.go`, introduced at commit `b28f0b6`. It shows that
the earlier `// codeql[...]` comment did not close the GitHub alert. The prior
expectation that a rescan alone would resolve it was incorrect.

CodeQL's Go password-hashing query treats the SHA-256 input as a sink; it does
not require that SHA-256's output be stored as the final verifier. Here the
output is immediately hex-encoded and supplied to `bcrypt.CompareHashAndPassword`.
No digest is returned, stored, logged or compared directly to authenticate.
Meteor accounts-password 3.3.1's `getPasswordString` performs the same SHA-256
preprocessing before its bcrypt verifier. Removing it would invalidate existing
Meteor credentials, including passwords longer than bcrypt's 72-byte limit.

An `AlertSuppression.ql` query can produce SARIF suppression metadata from
comments. That metadata is distinct from dismissing an alert in GitHub. The
unused suppression annotations have been removed to avoid implying that they
resolve default-setup findings. The hashing rule remains enabled, with no file
exclusions, renamed secret inputs or custom analysis barriers.

This is a reviewed false positive, not a newly discovered exploitable password
storage weakness. Existing lockout and account-policy checks continue to apply.
The Go bcrypt verifier tests cover two different salts, changed suffixes beyond
72 bytes, prehash-as-password attempts, and rejection of plaintext, raw SHA-256,
hexadecimal SHA-256 and malformed stored verifiers.

The maintainer can resolve [alert #3](https://github.com/wekan/wekango/security/code-scanning/3)
using **Dismiss alert → False positive**, with this review reason:

> This SHA-256 result is the Meteor compatibility prehash, not a stored password
> verifier. It is immediately hex-encoded and passed to salted bcrypt verification;
> only that comparison's boolean result is returned. Regression tests reject bare
> hashes, incorrect passwords and changes after byte 72. Removing the prehash would
> break existing Meteor accounts.

No remote alert has been dismissed, and no local CodeQL scan has been run. The
remaining action is the maintainer's disposition of this specific finding;
recompiling the same verifier is not expected to remove it automatically.

References: [CodeQL injection query](https://codeql.github.com/codeql-query-help/go/go-sql-injection/),
[password-hashing query implementation](https://github.com/github/codeql/blob/main/go/ql/src/Security/CWE-327/WeakSensitiveDataHashing.ql),
[Go suppression metadata query](https://github.com/github/codeql/blob/main/go/ql/src/AlertSuppression.ql),
[GitHub suppression-to-dismissal mechanism](https://github.com/advanced-security/dismiss-alerts),
and [Meteor password server](https://github.com/meteor/meteor/blob/release/METEOR%403.4.1/packages/accounts-password/password_server.js).

## Re-enabled accounts and concurrent blocking

`services.securityBlock` is historical refusal metadata, not an authentication
provider. Local bcrypt accounts can log in after being re-enabled while keeping
that metadata, their password hash and unrelated profile/email fields intact.
LDAP, two-factor authentication and unknown providers remain gated until ported.

The source `server/models/users.js` disable/enable actions assign empty strings,
but installed Collection2/SimpleSchema cleaning converts those dotted `$set`
values into `$unset`. Normal re-enabling therefore leaves `loginDisabled` absent;
normal disabling also removes `services.resume.loginTokens`. Accounts-base
recreates a missing token array with atomic `$addToSet`, which Go now preserves.
A raw imported string token field is malformed and is rejected, not silently
replaced. Existing arrays are never replaced from an earlier account snapshot.

The source login validation hook uses JavaScript truthiness. Go accepts missing,
null, false, empty-string and zero disabled flags, but rejects nonempty strings
(including `"false"`), nonzero numbers, arrays and objects. Both existing bearer
sessions and password login enforce this check. The direct Meteor local REST
password path bypasses its DDP validation hook; Go intentionally retains its
stronger disabled-account rejection for both paths.

Token issuance compares the observed disabled field and password/authentication
state in the same database update. A concurrent disable cannot append a token
after the account changes to a truthy flag. Real SQLite tests intercept the
outgoing token update, disable through an independent client, and verify rejection
and unchanged token count for boolean, string, array and object flags.

Concurrent successful logins also require database-level mutation isolation.
The previously pinned FerretDB v1.71.0 reads a document before opening its write
transaction, then replaces the whole document by `_id`. Two `$addToSet` calls
can consequently overwrite each other's tokens; a Go-only mutex would leave
other Mongo wire clients and embedded database tools exposed. The maintained
fork fix serializes mutation read/modify/write work in its shared handler.
See [the runtime source copy](Go-FerretDB-Source.md) for provenance and scope.
