package gcp

import (
	"fmt"
	"strings"
)

// Preferred Spot GPU targets for Phase 1: L4 first, T4 fallback.
const (
	GPUTypeL4 = "L4"
	GPUTypeT4 = "T4"

	MachineTypeL4 = "g2-standard-4" // 1× L4
	MachineTypeT4 = "n1-standard-4" // + 1× T4 accelerator

	DefaultZone   = "us-central1-a"
	DefaultRegion = "us-central1"
	ServerPort    = 8080
)

// GPUTarget describes a Spot GPU machine configuration.
type GPUTarget struct {
	Name           string // L4 | T4
	MachineType    string
	Accelerator    string // empty for G2 (L4 attached to machine type)
	AcceleratorCnt int
	DiskGB         int
	Reason         string
}

// DefaultTargets returns L4 then T4 Spot candidates.
func DefaultTargets() []GPUTarget {
	return []GPUTarget{
		{
			Name:        GPUTypeL4,
			MachineType: MachineTypeL4,
			DiskGB:      200,
			Reason:      "Spot L4 (g2-standard-4) — preferred Phase 1 GPU",
		},
		{
			Name:           GPUTypeT4,
			MachineType:    MachineTypeT4,
			Accelerator:    "nvidia-tesla-t4",
			AcceleratorCnt: 1,
			DiskGB:         200,
			Reason:         "Spot T4 fallback when L4 stock/quota is unavailable",
		},
	}
}

// PickTarget selects a GPU by name (L4/T4) or returns the preferred L4 default.
func PickTarget(want string) (GPUTarget, error) {
	want = strings.ToUpper(strings.TrimSpace(want))
	targets := DefaultTargets()
	if want == "" {
		return targets[0], nil
	}
	for _, t := range targets {
		if t.Name == want {
			return t, nil
		}
	}
	return GPUTarget{}, fmt.Errorf("unsupported --gpu %q (want L4 or T4)", want)
}

// ZoneFromRegion picks a default zone in the region when zone is empty.
func ZoneFromRegion(region, zone string) (string, string) {
	zone = strings.TrimSpace(zone)
	region = strings.TrimSpace(region)
	if zone != "" {
		if region == "" {
			parts := strings.Split(zone, "-")
			if len(parts) >= 2 {
				region = parts[0] + "-" + parts[1]
			}
		}
		return region, zone
	}
	if region == "" {
		return DefaultRegion, DefaultZone
	}
	// common "-a" zone; caller may override
	return region, region + "-a"
}
