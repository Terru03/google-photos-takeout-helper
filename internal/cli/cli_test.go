package cli

import (
	"reflect"
	"testing"
)

func TestParseWorkRootsUsesRepeatedFlagsAndPool(t *testing.T) {
	got := ParseWorkRoots(
		[]string{`C:\Takeout_Incoming`, " ", `E:\Takeout_Incoming`},
		`F:\Takeout_Incoming; ;G:\Takeout_Incoming`,
	)
	want := []string{
		`C:\Takeout_Incoming`,
		`E:\Takeout_Incoming`,
		`F:\Takeout_Incoming`,
		`G:\Takeout_Incoming`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestFirstCLIWorkRootKeepsSingleWorkBehavior(t *testing.T) {
	if got := firstCLIWorkRoot([]string{`C:\Takeout_Incoming`}); got != `C:\Takeout_Incoming` {
		t.Fatalf("got %q", got)
	}
}
