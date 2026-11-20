package config

import (
	"fmt"
	"os"
	"strings"
)

func secretFromEnv(name string) (string, error) {
	value := os.Getenv(name)
	fileName := strings.TrimSpace(os.Getenv(name + "_FILE"))
	if value != "" && fileName != "" {
		return "", fmt.Errorf("%s and %s_FILE are mutually exclusive", name, name)
	}
	if fileName == "" {
		return value, nil
	}
	content, err := os.ReadFile(fileName)
	if err != nil {
		return "", fmt.Errorf("read secret file for %s", name)
	}
	secret := strings.TrimRight(string(content), "\r\n")
	if secret == "" {
		return "", fmt.Errorf("secret file for %s is empty", name)
	}
	return secret, nil
}
