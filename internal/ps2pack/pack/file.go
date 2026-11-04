package pack

import "os"

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}

func fileSize(path string) (int, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return int(fi.Size()), nil
}
