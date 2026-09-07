package config

import (
	"path/filepath"
	"testing"
)

func TestBundlePaths(t *testing.T) {
	for _, tc := range []struct {
		name  string
		env   map[string]string
		files string
	}{
		{"default", nil, filepath.Join("bundle", "data", "files")},
		{"docker", map[string]string{"WRITABLE_PATH": "/data"}, "/data/files"},
		{"snap", map[string]string{"WRITABLE_PATH": "/var/snap/wekan/common/files"}, "/var/snap/wekan/common/files"},
		{"relative", map[string]string{"WRITABLE_PATH": ".."}, "../files"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, e := Load(func(k string) string { return tc.env[k] }, "bundle")
			if e != nil {
				t.Fatal(e)
			}
			if c.FilesPath != filepath.FromSlash(tc.files) || c.AttachmentsPath != filepath.Join(c.FilesPath, "attachments") || c.SQLiteDirectory != filepath.Join(c.FilesPath, "db") {
				t.Fatalf("layout: %+v", c)
			}
		})
	}
}
func TestOverrides(t *testing.T) {
	env := map[string]string{"PORT": "3000", "ROOT_URL": "https://example.com/wekan", "MONGO_URL": "mongodb://localhost/custom", "WITH_API": "false", "FERRETDB_SQLITE_DIR": "old-db", "FERRETDB_SQLITE_URL": "file:original/?mode=rw"}
	c, e := Load(func(k string) string { return env[k] }, ".")
	if e != nil {
		t.Fatal(e)
	}
	if c.Database != "custom" || c.Prefix != "/wekan" || c.WithAPI || c.SQLiteDirectory != "old-db" || c.SQLiteURL != env["FERRETDB_SQLITE_URL"] {
		t.Fatalf("%+v", c)
	}
}
func TestInvalidConfiguration(t *testing.T) {
	for k, values := range map[string][]string{"PORT": {"0", "65536", "wat"}, "ROOT_URL": {"javascript:alert(1)", "http://user:pass@example.com", "https://example.com/a/../b"}, "BIND_IP": {"not-an-ip"}, "MONGO_URL": {"https://example.com"}} {
		for _, v := range values {
			_, e := Load(func(key string) string {
				if key == k {
					return v
				}
				return ""
			}, ".")
			if e == nil {
				t.Errorf("accepted %s=%s", k, v)
			}
		}
	}
}
