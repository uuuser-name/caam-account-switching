package rotation

import (
	"math/rand"
	"testing"
)

// BenchmarkSelectorSmall benchmarks selection with a small number of profiles.
func BenchmarkSelectorSmall(b *testing.B) {
	profiles := []string{"alice", "bob", "charlie"}

	s := NewSelector(AlgorithmSmart, nil, nil)
	s.SetRNG(rand.New(rand.NewSource(42)))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := s.Select("claude", profiles, "")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSelectorMedium benchmarks selection with a medium number of profiles.
func BenchmarkSelectorMedium(b *testing.B) {
	profiles := make([]string, 10)
	for i := range profiles {
		profiles[i] = "profile" + string(rune('A'+i))
	}

	s := NewSelector(AlgorithmSmart, nil, nil)
	s.SetRNG(rand.New(rand.NewSource(42)))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := s.Select("claude", profiles, "")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSelectorLarge benchmarks selection with a large number of profiles.
func BenchmarkSelectorLarge(b *testing.B) {
	profiles := make([]string, 50)
	for i := range profiles {
		profiles[i] = "profile" + string(rune('A'+i%26)) + string(rune('0'+i/26))
	}

	s := NewSelector(AlgorithmSmart, nil, nil)
	s.SetRNG(rand.New(rand.NewSource(42)))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := s.Select("claude", profiles, "")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSelectorRoundRobin benchmarks round-robin selection.
func BenchmarkSelectorRoundRobin(b *testing.B) {
	profiles := []string{"alice", "bob", "charlie", "dave", "eve"}

	s := NewSelector(AlgorithmRoundRobin, nil, nil)
	current := ""

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := s.Select("claude", profiles, current)
		if err != nil {
			b.Fatal(err)
		}
		current = result.Selected
	}
}

// BenchmarkSelectorRandom benchmarks random selection.
func BenchmarkSelectorRandom(b *testing.B) {
	profiles := []string{"alice", "bob", "charlie", "dave", "eve"}

	s := NewSelector(AlgorithmRandom, nil, nil)
	s.SetRNG(rand.New(rand.NewSource(42)))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := s.Select("claude", profiles, "")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSelectorWithExclusion benchmarks selection with current profile exclusion.
func BenchmarkSelectorWithExclusion(b *testing.B) {
	profiles := []string{"alice", "bob", "charlie", "dave", "eve"}

	s := NewSelector(AlgorithmSmart, nil, nil)
	s.SetRNG(rand.New(rand.NewSource(42)))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Always exclude the first profile
		_, err := s.Select("claude", profiles, "alice")
		if err != nil {
			b.Fatal(err)
		}
	}
}
