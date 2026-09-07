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

The annotation at the single lookup is scoped to the SQL/NoSQL injection rule.
It documents the typed BSON boundary; it does not disable analysis elsewhere.
Tests inspect the actual BSON encoding and send JSON and form operator attempts
to a real embedded database. They verify that no session is issued and that
legitimate literal names containing metacharacters still work.

## Existing Meteor passwords

Meteor stores salted bcrypt verifiers over the hexadecimal SHA-256 prehash of
the password. Removing that prehash would break existing accounts. SHA-256 by
itself is not the stored password verifier: `compareMeteorPassword` always
checks the expensive bcrypt verifier and returns only a boolean.

The annotation at this one intermediate hash documents that required composition.
It does not exempt other hashes or weaken the bcrypt comparison. Tests prove
that distinct salts work, changed suffixes beyond 72 bytes are rejected, and
plaintext, raw SHA-256, hexadecimal SHA-256 and malformed hashes cannot serve
as password verifiers. Presenting the prehash as the password also fails.

These are reviewed false positives, not newly discovered exploitable
vulnerabilities; they have no new incident category or automatic account block.
Existing lockout and account policy checks continue to apply. No remote alert
was dismissed. GitHub must rerun CodeQL after the maintainer publishes the code
to verify the annotations in its analysis environment.

References: [CodeQL injection query](https://codeql.github.com/codeql-query-help/go/go-sql-injection/),
[password hashing query](https://codeql.github.com/codeql-query-help/go/go-weak-sensitive-data-hashing/)
and [query-specific suppression support](https://codeql.github.com/docs/codeql-overview/codeql-changelog/codeql-cli-2.12.0/).
