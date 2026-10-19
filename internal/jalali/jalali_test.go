package jalali

import "testing"

func TestToJalali(t *testing.T) {
	for _, c := range []struct{ gy, gm, gd, jy, jm, jd int }{
		{2026, 3, 21, 1405, 1, 1},   // Nowruz 1405
		{2026, 10, 18, 1405, 7, 26}, // Mehr has 30 days, from 23 September
		{2025, 3, 20, 1403, 12, 30}, // the last day of a leap year
		{2026, 3, 20, 1404, 12, 29}, // and of a common one
	} {
		jy, jm, jd := ToJalali(c.gy, c.gm, c.gd)
		if jy != c.jy || jm != c.jm || jd != c.jd {
			t.Errorf("ToJalali(%d-%02d-%02d) = %d/%02d/%02d, want %d/%02d/%02d", c.gy, c.gm, c.gd, jy, jm, jd, c.jy, c.jm, c.jd)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	for _, g := range [][3]int{{2026, 8, 16}, {2024, 2, 29}, {2000, 1, 1}, {2031, 12, 31}} {
		jy, jm, jd := ToJalali(g[0], g[1], g[2])
		gy, gm, gd := FromJalali(jy, jm, jd)
		if gy != g[0] || gm != g[1] || gd != g[2] {
			t.Errorf("%v came back as %d-%d-%d", g, gy, gm, gd)
		}
	}
}

func TestIsLeap(t *testing.T) {
	if !IsLeap(1403) || IsLeap(1404) || IsLeap(1405) {
		t.Errorf("1403 is leap, 1404 and 1405 are not: got %v %v %v", IsLeap(1403), IsLeap(1404), IsLeap(1405))
	}
}
