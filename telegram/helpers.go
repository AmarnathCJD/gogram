package telegram

import "os"

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func PathIsWritable(path string) bool {
	file, err := os.OpenFile(path, os.O_WRONLY, 0666)
	if err != nil {
		return false
	}
	defer file.Close()
	return true
}
