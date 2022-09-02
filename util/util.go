package util

import (
	"io/ioutil"
	"os"

	"github.com/wonderivan/logger"
)

func ReadFile(fileName string) []byte {
	f, err := os.OpenFile(fileName, os.O_RDONLY, 0600)
	if err != nil {
		logger.Error("Open file %s failed", fileName)
		return nil
	}

	defer f.Close()
	s, err := ioutil.ReadAll(f)
	if err != nil {
		logger.Error("Read file %s content error[%s]", fileName, err)
		return nil
	}

	return s
}
