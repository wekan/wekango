# WeKan Go

An incremental Go implementation of [WeKan](https://github.com/wekan/wekan),
aiming to be a drop-in replacement distributed as one native executable.
Caddy, the application server and FerretDB's SQLite implementation run in one
process; external MongoDB/FerretDB remains available through `MONGO_URL`.

**This is a compatibility preview, not yet a production replacement.**
[ROADMAP.md](ROADMAP.md) records tested progress and remaining behavior.
[Go compatibility](docs/Go-Compatibility.md) explains paths and current commands.

```sh
./build.sh test
./build.sh build
PORT=8080 ROOT_URL=http://localhost:8080 ./dist/wekan-<platform>
```

Use `./build.sh help` for the exact build output and platform commands.
The preview reads existing local user credentials and boards; it does not seed an
administrator or migrate all historical data automatically. Use test copies of
existing data while compatibility work continues.

[Dependencies](DEPENDENCIES.md) and [third-party notices](THIRD_PARTY_NOTICES.md)
document the non-GPL, MIT-compatible dependency policy and retained obligations.
Releases use `wekan-<platform>` filenames at
[wekan/wekango](https://github.com/wekan/wekango/releases).
