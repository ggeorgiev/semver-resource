package driver

import "testing"

func TestGitDriver_CurrentVersion(t *testing.T) {
	empty, err := currentVersion("v", "")
	if err != nil {
		t.Error(err)
	}
	if empty.String() != "0.0.0" {
		t.Logf("Expected: \"\", Actual: %s", empty.String())
		t.Fail()
	}

	sample, err := currentVersion("v", `v2.10.0
v2.9.4
v2.9.3
v2.10.2
v2.10.1
v2.9.2
v2.9.1
v2.9.0
v2.8.2
v2.8.1
`)
	if err != nil {
		t.Error(err)
	}
	if sample.String() != "2.10.2" {
		t.Logf("Expected: \"2.10.2\", Actual: %s", sample.String())
		t.Fail()
	}
}
