package cli

import "testing"

func TestClassifyJobKey(t *testing.T) {
	if classifyJobKey('v') != "verbose" || classifyJobKey('V') != "verbose" {
		t.Fatal("v")
	}
	if classifyJobKey('h') != "hurry" {
		t.Fatal("h")
	}
	if classifyJobKey('c') != "cancel" || classifyJobKey(3) != "cancel" {
		t.Fatal("c")
	}
	if classifyJobKey('\n') != "" || classifyJobKey('x') != "" {
		t.Fatal("ignore")
	}
}
