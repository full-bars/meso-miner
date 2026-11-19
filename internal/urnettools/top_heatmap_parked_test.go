package urnettools

import (
	"testing"

	"github.com/urnetwork/connect"
	"github.com/urnetwork/connect/internal/tui"
)

func TestProxyGridParkedAndActive(t *testing.T) {
	for _, tc := range []struct {
		name   string
		states []uint8
		want   rune
	}{
		{"mixed", []uint8{connect.ProxyCellParked, connect.ProxyCellUp}, '█'},
		{"parked", []uint8{connect.ProxyCellParked, connect.ProxyCellParked}, '█'},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(tui.MonoTheme(), topProviders(1), &fakeClock{t: topBase})
			m.proxyCells = makeProxyCells(tc.states)
			b := tui.New(1, 2)
			m.drawProxyGrid(b)
			if got := b.Cell(0, 0).Rune; got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
