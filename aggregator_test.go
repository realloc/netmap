package netmap

import (
	"math"
	"math/rand"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	eps float64 = 0.001
)

func initTestBucket(t *testing.T, b *Bucket) {
	require.Nil(t, b.AddBucket("/opt:first", Nodes{{0, 1, 2}, {2, 3, 2}}))
	require.Nil(t, b.AddBucket("/opt:second/sub:1", Nodes{{1, 2, 3}, {10, 6, 1}}))

	b.fillNodes()
}

func TestNewWeightFunc(t *testing.T) {
	var b Bucket

	initTestBucket(t, &b)

	meanCap := b.Traverse(new(meanAgg), CapWeightFunc).Compute()
	capNorm := NewSigmoidNorm(meanCap)

	minPrice := b.Traverse(new(minAgg), PriceWeightFunc).Compute()
	priceNorm := NewReverseMinNorm(minPrice)

	wf := NewWeightFunc(capNorm, priceNorm)

	nodes := make(Nodes, len(b.nodes))
	copy(nodes, b.nodes)

	expected := Nodes{
		{10, 6, 1},
		{2, 3, 2},
		{1, 2, 3},
		{0, 1, 2},
	}

	sort.Slice(nodes, func(i, j int) bool { return wf(nodes[i]) > wf(nodes[j]) })
	require.Equal(t, expected, nodes)
}

func TestAggregator_Compute(t *testing.T) {
	var (
		b Bucket
		a Aggregator
	)

	initTestBucket(t, &b)

	a = NewMeanAgg()
	b.Traverse(a, CapWeightFunc)
	require.InEpsilon(t, 3.0, a.Compute(), eps)

	a = NewMeanSumAgg()
	b.Traverse(a, CapWeightFunc)
	require.InEpsilon(t, 3.0, a.Compute(), eps)

	a = NewMinAgg()
	b.Traverse(a, PriceWeightFunc)
	require.InEpsilon(t, 1.0, a.Compute(), eps)

	a = NewMaxAgg()
	b.Traverse(a, PriceWeightFunc)
	require.InEpsilon(t, 3.0, a.Compute(), eps)

	a = NewMeanIQRAgg()
	b.Traverse(a, PriceWeightFunc)
	require.InEpsilon(t, 2.0, a.Compute(), eps)

	mp := new(meanIQRAgg)
	nodes := []Node{{P: 1}, {P: 1}, {P: 10}, {P: 3}, {P: 5}, {P: 5}, {P: 1}, {P: 100}}
	for i := range nodes {
		mp.Add(PriceWeightFunc(nodes[i]))
	}

	mp.k = 0.5
	require.InEpsilon(t, 2.666, mp.Compute(), eps)

	mp.k = 1.5
	require.InEpsilon(t, 3.714, mp.Compute(), eps)

	mp.k = 23.0
	require.InEpsilon(t, 3.714, mp.Compute(), eps)

	mp.k = 24.0
	require.InEpsilon(t, 15.75, mp.Compute(), eps)

	mp.Clear()
	mp.Add(1)
	mp.Add(101)
	require.InEpsilon(t, 51.0, mp.Compute(), eps)
}

func TestSigmoidNorm_Normalize(t *testing.T) {
	t.Run("sigmoid norm must equal to 1/2 at `scale`", func(t *testing.T) {
		norm := NewSigmoidNorm(1)
		require.InEpsilon(t, 0.5, norm.Normalize(1), eps)

		norm = NewSigmoidNorm(10)
		require.InEpsilon(t, 0.5, norm.Normalize(10), eps)
	})

	t.Run("sigmoid norm must be less than 1", func(t *testing.T) {
		norm := NewSigmoidNorm(2)
		require.True(t, norm.Normalize(100) < 1)
		require.True(t, norm.Normalize(math.MaxFloat64) <= 1)
	})

	t.Run("sigmoid norm must be monotonic", func(t *testing.T) {
		norm := NewSigmoidNorm(5)
		for i := 0; i < 5; i++ {
			a, b := rand.Float64(), rand.Float64()
			if b < a {
				a, b = b, a
			}
			require.True(t, norm.Normalize(a) <= norm.Normalize(b))
		}
	})
}

func TestReverseMinNorm_Normalize(t *testing.T) {
	t.Run("reverseMin norm should not panic", func(t *testing.T) {
		norm := NewReverseMinNorm(0)
		require.NotPanics(t, func() { norm.Normalize(0) })

		norm = NewReverseMinNorm(1)
		require.NotPanics(t, func() { norm.Normalize(0) })
	})

	t.Run("reverseMin norm should equal 1 at min value", func(t *testing.T) {
		norm := NewReverseMinNorm(10)
		require.InEpsilon(t, 1.0, norm.Normalize(10), eps)
	})
}

func TestMaxNorm_Normalize(t *testing.T) {
	t.Run("max norm should not panic", func(t *testing.T) {
		norm := NewMaxNorm(0)
		require.NotPanics(t, func() { norm.Normalize(1) })

		norm = NewMaxNorm(1)
		require.NotPanics(t, func() { norm.Normalize(0) })
	})

	t.Run("max norm should equal 1 at max value", func(t *testing.T) {
		norm := NewMaxNorm(10)
		require.InEpsilon(t, 1.0, norm.Normalize(10), eps)
	})
}

func TestBucket_TraverseTree(t *testing.T) {
	var (
		meanAF = AggregatorFactory{New: func() Aggregator { return new(meanAgg) }}
		minAF  = AggregatorFactory{New: func() Aggregator { return new(minAgg) }}
	)

	b := &Bucket{
		children: []Bucket{
			{nodes: Nodes{{0, 1, 2}, {2, 3, 2}}},
			{
				children: []Bucket{
					{nodes: Nodes{{1, 2, 3}, {10, 6, 1}}},
					{nodes: Nodes{{12, 3, 4}, {2, 3, 4}}},
				},
			},
		},
	}
	b.fillNodes()

	b.TraverseTree(meanAF, CapWeightFunc)
	require.InEpsilon(t, 2, b.children[0].weight, eps)
	require.InEpsilon(t, 3.5, b.children[1].weight, eps)
	require.InEpsilon(t, 4, b.children[1].children[0].weight, eps)
	require.InEpsilon(t, 3, b.children[1].children[1].weight, eps)

	b.TraverseTree(minAF, PriceWeightFunc)
	require.InEpsilon(t, 2, b.children[0].weight, eps)
	require.InEpsilon(t, 1, b.children[1].weight, eps)
	require.InEpsilon(t, 1, b.children[1].children[0].weight, eps)
	require.InEpsilon(t, 4, b.children[1].children[1].weight, eps)
}
