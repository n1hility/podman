package specgen

import (
	"fmt"
	"os"
)

func adjustVolumeSplit(splitVol []string) ([]string, bool) {
	fmt.Printf("%v", splitVol)
	if len(splitVol[0]) == 1 {
		drivePath := splitVol[0] + ":" + splitVol[1]
		fmt.Println("Drive Path = " + drivePath)
		if _, err := os.Stat(drivePath); err == nil {
			splitVol = splitVol[1:]
			splitVol[0] = drivePath
			fmt.Printf("Fix: %v", splitVol)
			return splitVol, true
		}
	}

	return splitVol, false
}