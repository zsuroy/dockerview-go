package main

import (
	"fmt"
	"net/http"
	"runtime"

	"github.com/minio/selfupdate"
)

func doUpdate() error {
	repo := "zsuroy/dockerview-go"
	assetsName := fmt.Sprintf("dockerview-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		assetsName += ".exe"
	}

	url := fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", repo, assetsName)
	fmt.Printf("Donwloading from %s...\n", url)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("Download failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Server error: %s", resp.Status)
	}

	err = selfupdate.Apply(resp.Body, selfupdate.Options{})
	if err != nil {
		return fmt.Errorf("Apply update failed: %v", err)
	}

	fmt.Println("Update done! Please restart it.")
	return nil

}
