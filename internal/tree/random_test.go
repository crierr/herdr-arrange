package tree

import (
	"fmt"
	"math/rand"

	"github.com/crierr/herdr-arrange/internal/herdr"
)

// Deterministic randomness: a failing seed can be replayed exactly.
type rng struct{ *rand.Rand }

func newRand(seed int64) *rng { return &rng{rand.New(rand.NewSource(seed))} }

func (g *rng) intn(n int) int { return g.Rand.Intn(n) }

func (g *rng) dir() herdr.SplitDirection {
	if g.intn(2) == 0 {
		return herdr.Right
	}
	return herdr.Down
}

// ratio returns a ratio inside herdr's clamp, so a tree built with it is
// actually reachable.
func (g *rng) ratio() float64 {
	return MinRatio + g.Float64()*(MaxRatio-MinRatio)
}

func paneName(i int) string { return fmt.Sprintf("p%d", i) }

// randomTree builds an arbitrarily-shaped tree over n distinct panes.
func randomTree(g *rng, n int) *Node {
	panes := make([]string, n)
	for i := range panes {
		panes[i] = paneName(i)
	}
	return randomTreeOver(g, panes)
}

// randomTreeOver builds an arbitrarily-shaped tree over exactly these panes,
// keeping them in the given order so callers can also shuffle to get a
// permutation.
func randomTreeOver(g *rng, panes []string) *Node {
	if len(panes) == 1 {
		return Leaf(panes[0])
	}
	split := 1 + g.intn(len(panes)-1)
	return Split(g.dir(), g.ratio(),
		randomTreeOver(g, panes[:split]),
		randomTreeOver(g, panes[split:]))
}

// shuffled returns a permutation of panes.
func (g *rng) shuffled(panes []string) []string {
	out := append([]string(nil), panes...)
	g.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}
