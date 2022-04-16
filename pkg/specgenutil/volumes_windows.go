package specgenutil

import (
	"path/filepath"
	"github.com/sirupsen/logrus"
)

func resolveRelativeOnWindows(path string) string {
	ret, err := filepath.Abs(path)
	if err != nil {
		logrus.Debugf("problem resolving possible relative path %q: %s", path, err.Error())
		return path
	}

	return ret
}

func shouldResolveUnixWinVariant(path string) bool {
	return true
}