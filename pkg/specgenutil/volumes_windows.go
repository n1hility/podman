package specgenutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

func convertMountPath(path string) (string, error) {
	if strings.HasPrefix(path, "/") {
		// Handle /[driveletter]/windows/path form (e.g. c:\Users\bar == /c/Users/bar)
		if len(path) > 2 && path[2] == '/' {
			drive := unicode.ToLower(rune(path[1])) 
			if unicode.IsLetter(drive) && drive <= unicode.MaxASCII {
				winPath := fmt.Sprintf("%c:%s", drive,  strings.ReplaceAll(path[2:], "/", `\`))
				if _, err := os.Stat(winPath); err == nil {
					return fmt.Sprintf("/mnt/%c/%s", drive, path[3:]), nil
				}
			}
		} 

		// unix path - pass through 
		return path, nil
	}

	path, err := filepath.Abs(path); 
	if err != nil {
		return "", err
	}

	if strings.HasPrefix(path, `\\.\`) {
		path = "/mnt/wsl/" + path[4:]
	} else if path[1] == ':' {
		path = "/mnt/" + strings.ToLower(path[0:1]) + path[2:]		
	} else {
		return path, errors.New("unsupported UNC path")
	}

	return strings.ReplaceAll(path, `\`, "/"), nil
}