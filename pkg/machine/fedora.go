// +build amd64 arm64

package machine

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	githubURL      = "http://github.com/fedora-cloud/docker-brew-fedora/"
	defaultRelease = "35"
)

type FedoraDownload struct {
	Download
}

func NewFedoraDownloader(vmType, vmName, releaseStream string) (DistributionDownload, error) {
	imageName, downloadUrl, size, err := getFedoraDownload(releaseStream)
	if err != nil {
		return nil, err
	}

	dataDir, err := GetDataDir(vmType)
	if err != nil {
		return nil, err
	}

	f := FedoraDownload{
		Download: Download{
			Arch:      getFcosArch(),
			Artifact:  artifact,
			Format:    Format,
			ImageName: imageName,
			LocalPath: filepath.Join(dataDir, imageName),
			URL:       downloadUrl,
			VMName:    vmName,
			Size:      size,
		},
	}
	f.Download.LocalUncompressedFile = f.getLocalUncompressedName()
	return f, nil
}

func (f FedoraDownload) Get() *Download {
	return &f.Download
}

func (d FedoraDownload) UpdateAvailable() (bool, error) {
	info, err := os.Stat(d.LocalPath)
	if err != nil {
		return false, nil
	}

	return info.Size() == d.Size, nil
}

func truncRead(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			logrus.Error(err)
		}
	}()

	body, err := ioutil.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, err
	}

	_, _ = io.Copy(io.Discard, resp.Body)

	return body, nil
}

func getFedoraDownload(releaseStream string) (string, *url.URL, int64, error) {
	release := defaultRelease
	if len(releaseStream) > 0 && releaseStream != "testing" {
		release = releaseStream
	}

	dirUrl := githubURL + "tree/" + release + "/" + getFcosArch() + "/"
	body, err := truncRead(dirUrl)
	if err != nil {
		return "", nil, -1, err
	}

	rx, err := regexp.Compile(`fedora[^\"]+xz`)
	if err != nil {
		return "", nil, -1, err
	}
	file := rx.FindString(string(body))
	if len(file) <= 0 {
		return "", nil, -1, fmt.Errorf("Could not locate Fedora download at %s", dirUrl)
	}

	rawUrl := githubURL + "raw/" + release + "/" + getFcosArch() + "/"
	newLocation := rawUrl + file
	downloadUrl, err := url.Parse(newLocation)
	if err != nil {
		return "", nil, -1, errors.Wrapf(err, "Invalid URL generated from discovered Fedora file: %s", newLocation)
	}

	resp, err := http.Head(newLocation)
	if err != nil {
		return "", nil, -1, errors.Wrapf(err, "Head request failed: %s", newLocation)
	}

	if resp.StatusCode != http.StatusOK {
		return "", nil, -1, fmt.Errorf("Head request failed [%d] on download: %s", resp.StatusCode, newLocation)
	}

	return file, downloadUrl, resp.ContentLength, nil
}
