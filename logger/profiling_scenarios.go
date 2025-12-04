package logger

import "fmt"

// ProfilingConfig represents a single test configuration
type ProfilingConfig struct {
	Threads    int
	Shards     int
	BufferSize int
	Name       string
}

// GetProfilingScenarios returns all ~40 targeted test scenarios
func GetProfilingScenarios() []ProfilingConfig {
	scenarios := make([]ProfilingConfig, 0, 45)
	
	// Set A: Baseline - All thread counts with shards=[8,16], buffer=8MB (16 tests)
	threads := []int{1, 2, 4, 8, 10, 20, 50, 100}
	shards := []int{8, 16}
	bufferSize := 8 * 1024 * 1024 // 8MB
	
	for _, t := range threads {
		for _, s := range shards {
			scenarios = append(scenarios, ProfilingConfig{
				Threads:    t,
				Shards:     s,
				BufferSize: bufferSize,
				Name:       formatName("SetA-Baseline", t, s, bufferSize),
			})
		}
	}
	
	// Set B: Shard Scaling - threads=[50,100] with all shard counts, buffer=8MB (12 tests)
	highThreads := []int{50, 100}
	allShards := []int{1, 2, 4, 8, 16, 32}
	
	for _, t := range highThreads {
		for _, s := range allShards {
			scenarios = append(scenarios, ProfilingConfig{
				Threads:    t,
				Shards:     s,
				BufferSize: bufferSize,
				Name:       formatName("SetB-ShardScaling", t, s, bufferSize),
			})
		}
	}
	
	// Set C: Buffer Sizing - threads=50, shards=16, all buffer sizes (4 tests)
	bufferSizes := []int{
		2 * 1024 * 1024,  // 2MB
		4 * 1024 * 1024,  // 4MB
		8 * 1024 * 1024,  // 8MB
		16 * 1024 * 1024, // 16MB
	}
	
	for _, bs := range bufferSizes {
		scenarios = append(scenarios, ProfilingConfig{
			Threads:    50,
			Shards:     16,
			BufferSize: bs,
			Name:       formatName("SetC-BufferSizing", 50, 16, bs),
		})
	}
	
	// Set D: Edge Cases - Critical boundary conditions (8 tests)
	edgeCases := []ProfilingConfig{
		// Minimum config
		{
			Threads:    1,
			Shards:     1,
			BufferSize: 2 * 1024 * 1024,
			Name:       "SetD-EdgeCase-MinConfig-1T-1S-2MB",
		},
		// Over-sharded low concurrency
		{
			Threads:    1,
			Shards:     32,
			BufferSize: 16 * 1024 * 1024,
			Name:       "SetD-EdgeCase-OverSharded-1T-32S-16MB",
		},
		// Under-sharded high concurrency
		{
			Threads:    100,
			Shards:     1,
			BufferSize: 2 * 1024 * 1024,
			Name:       "SetD-EdgeCase-UnderSharded-100T-1S-2MB",
		},
		// Maximum config
		{
			Threads:    100,
			Shards:     32,
			BufferSize: 16 * 1024 * 1024,
			Name:       "SetD-EdgeCase-MaxConfig-100T-32S-16MB",
		},
		// Typical production
		{
			Threads:    50,
			Shards:     8,
			BufferSize: 4 * 1024 * 1024,
			Name:       "SetD-EdgeCase-TypicalProd-50T-8S-4MB",
		},
		// High threads, moderate shards, small buffer (stress test)
		{
			Threads:    100,
			Shards:     8,
			BufferSize: 2 * 1024 * 1024,
			Name:       "SetD-EdgeCase-StressTest-100T-8S-2MB",
		},
		// Balanced high-throughput
		{
			Threads:    50,
			Shards:     32,
			BufferSize: 8 * 1024 * 1024,
			Name:       "SetD-EdgeCase-Balanced-50T-32S-8MB",
		},
		// Low threads, many shards (over-provisioned)
		{
			Threads:    10,
			Shards:     32,
			BufferSize: 8 * 1024 * 1024,
			Name:       "SetD-EdgeCase-OverProvisioned-10T-32S-8MB",
		},
	}
	
	scenarios = append(scenarios, edgeCases...)
	
	return scenarios
}

// formatName creates a consistent naming format for scenarios
func formatName(setName string, threads, shards, bufferMB int) string {
	bufferSizeMB := bufferMB / (1024 * 1024)
	return fmt.Sprintf("%s-T%d-S%d-B%dMB", setName, threads, shards, bufferSizeMB)
}

// GetScenariosBySet returns scenarios grouped by test set
func GetScenariosBySet() map[string][]ProfilingConfig {
	allScenarios := GetProfilingScenarios()
	sets := make(map[string][]ProfilingConfig)
	
	for _, scenario := range allScenarios {
		setName := "Unknown"
		if len(scenario.Name) >= 4 {
			if scenario.Name[:4] == "SetA" {
				setName = "SetA-Baseline"
			} else if scenario.Name[:4] == "SetB" {
				setName = "SetB-ShardScaling"
			} else if scenario.Name[:4] == "SetC" {
				setName = "SetC-BufferSizing"
			} else if scenario.Name[:4] == "SetD" {
				setName = "SetD-EdgeCases"
			}
		}
		
		sets[setName] = append(sets[setName], scenario)
	}
	
	return sets
}

// GetScenarioCount returns the total number of scenarios
func GetScenarioCount() int {
	return len(GetProfilingScenarios())
}

// GetEstimatedDuration returns estimated total runtime
func GetEstimatedDuration() string {
	count := GetScenarioCount()
	minutes := count * 2 // 2 minutes per test
	return fmt.Sprintf("%d tests × 2 min = ~%d minutes", count, minutes)
}

