package docker

import "github.com/docker/docker/api/types/volume"

// volSummary builds a *volume.Volume with UsageData for tests.
func volSummary(name, driver, mountpoint string, size, refCount int64, createdAt string) *volume.Volume {
	return &volume.Volume{
		Name:       name,
		Driver:     driver,
		Mountpoint: mountpoint,
		CreatedAt:  createdAt,
		UsageData:  &volume.UsageData{Size: size, RefCount: refCount},
	}
}

// volSummaryNoUsage builds a volume without UsageData (driver didn't report it).
func volSummaryNoUsage(name, driver, mountpoint string) *volume.Volume {
	return &volume.Volume{
		Name:       name,
		Driver:     driver,
		Mountpoint: mountpoint,
	}
}
