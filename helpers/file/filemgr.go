package file

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
)

// ListFile list file in directory
func ListFile(dir string, fType string) (matches []string) {
	files, err := filepath.Glob(fmt.Sprintf("%s/*", dir))
	if err != nil {
		panic(err)
	}
	list := []string{}
	if len(files) > 0 {
		for _, t := range files {
			fi, err := os.Stat(t)
			if err != nil {
				panic(err)
			}

			switch mode := fi.Mode(); {
			case mode.IsDir():
				ex := ListFile(t, fType)
				list = append(list, ex...)
			case mode.IsRegular():
				// do file stuff
				if fType != "" && path.Ext(t) == fType {
					list = append(list, t)
				}
			}
		}
	}

	return list
}
