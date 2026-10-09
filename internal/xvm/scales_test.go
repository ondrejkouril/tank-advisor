package xvm

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestParseScales: the live file of 2026-10-09.
func TestParseScales(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "xvm", "xvmscales-wg.json"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseScales(raw)
	if err != nil {
		t.Fatal(err)
	}
	if s.WN8[0] != 185 || s.WN8[99] != 4881 || s.WinRate[0] != 42.14 || s.WinRate[99] != 70.55 {
		t.Errorf("ends = %v..%v, %v..%v", s.WN8[0], s.WN8[99], s.WinRate[0], s.WinRate[99])
	}
	for _, tc := range []struct {
		value float64
		want  int
	}{{100, 0}, {185, 1}, {1700, 50}, {1701, 51}, {5000, 100}} {
		if got := Rank(s.WN8, tc.value); got != tc.want {
			t.Errorf("Rank(WN8 %v) = %d, want %d", tc.value, got, tc.want)
		}
	}
}

func TestParseScalesRefusesBrokenFiles(t *testing.T) {
	for name, in := range map[string]string{
		"not json":   `<html>`,
		"short":      `{"xwn8":[1,2,3],"xwin":[1,2,3]}`,
		"no winrate": `{"xwn8":[` + steps(false) + `]}`,
		"falling":    `{"xwn8":[` + steps(true) + `],"xwin":[` + steps(false) + `]}`,
	} {
		if _, err := ParseScales([]byte(in)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func steps(falling bool) string {
	out := ""
	for i := range 100 {
		v := i
		if falling {
			v = 100 - i
		}
		if i > 0 {
			out += ","
		}
		out += strconv.Itoa(v)
	}
	return out
}
