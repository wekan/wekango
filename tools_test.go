package main

import "testing"

func TestDatabaseToolDispatch(t *testing.T) {
	for _, name := range []string{"bsondump", "mongodump", "mongorestore", "mongoexport", "mongoimport", "mongofiles", "mongostat", "mongotop"} {
		for _, argv := range [][]string{{"wekan-arm64", name, "--version"}, {"/opt/bin/" + name, "--version"}, {"/opt/bin/" + name + ".exe", "--version"}} {
			got, args, ok := databaseToolCommand(argv)
			if !ok || got != name || len(args) != 1 || args[0] != "--version" {
				t.Fatalf("dispatch %v: %s %v %v", argv, got, args, ok)
			}
		}
	}
	for _, argv := range [][]string{nil, {"wekan-arm64"}, {"wekan-arm64", "--version"}, {"wekan-arm64", "not-a-tool"}} {
		if _, _, ok := databaseToolCommand(argv); ok {
			t.Fatalf("unexpected tool: %v", argv)
		}
	}
}
func TestDatabaseToolConnectionSelection(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"--version"}, {"--uri", "mongodb://example"}, {"--uri=mongodb://example"}, {"--host=example"}, {"--host", "example"}, {"-h", "example"}, {"-hexample"}, {"--port", "12345"}, {"--config", "config.yaml"}, {"mongodb://example/db"}, {"mongodb+srv://example/db"}} {
		if toolUsesConfiguredDatabase("mongodump", args) {
			t.Fatalf("overrode own target/help: %v", args)
		}
	}
	if toolUsesConfiguredDatabase("bsondump", nil) {
		t.Fatal("BSON conversion started database")
	}
	if !toolUsesConfiguredDatabase("mongodump", []string{"--archive=backup.bson", "--db", "wekan"}) {
		t.Fatal("default tools did not use existing storage")
	}
}
