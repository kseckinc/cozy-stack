package dynamic

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/cozy/cozy-stack/pkg/assets/model"
	"github.com/cozy/cozy-stack/pkg/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/ncw/swift/v2"
)

// ErrDynAssetNotFound is the error returned when a dynamic asset cannot be
// found.
var ErrDynAssetNotFound = errors.New("Dynamic asset was not found")

var assetsClient = &http.Client{
	Timeout: 30 * time.Second,
}

// CheckStatus checks that the FS for dynamic asset is available, or returns an
// error if it is not the case. It also returns the latency.
func CheckStatus() (time.Duration, error) {
	if assetFS == nil {
		return 0, nil
	}
	return assetFS.CheckStatus()
}

// ListAssets returns the list of the dynamic assets.
func ListAssets() (map[string][]*model.Asset, error) {
	return assetFS.List()
}

// GetAsset retrieves a raw asset from the dynamic FS and builds a fs.Asset
func GetAsset(context, name string) (*model.Asset, error) {
	// In unit tests, the assetFS is often not initialized
	if assetFS == nil {
		return nil, ErrDynAssetNotFound
	}

	// Re-constructing the asset struct from the dyn FS content
	content, err := assetFS.Get(context, name)
	if err != nil {
		if err == swift.ObjectNotFound || os.IsNotExist(err) {
			return nil, ErrDynAssetNotFound
		}
		return nil, err
	}

	h := sha256.New()
	_, err = h.Write(content)
	if err != nil {
		return nil, err
	}
	suma := h.Sum(nil)
	sumx := hex.EncodeToString(suma)

	buf := new(bytes.Buffer)
	bw := brotli.NewWriter(buf)
	if _, err = bw.Write(content); err != nil {
		return nil, err
	}
	if err = bw.Close(); err != nil {
		return nil, err
	}
	brotliContent := buf.Bytes()

	asset := model.NewAsset(model.AssetOption{
		Shasum:   sumx,
		Name:     name,
		Context:  context,
		IsCustom: true,
	}, content, brotliContent)

	return asset, nil
}

// RemoveAsset removes a dynamic asset from Swift
func RemoveAsset(context, name string) error {
	return assetFS.Remove(context, name)
}

// RegisterCustomExternals ensures that the assets are in the Swift, and load
// them from their source if they are not yet available.
func RegisterCustomExternals(opts []model.AssetOption, maxTryCount int) error {
	if len(opts) == 0 {
		return nil
	}

	assetsCh := make(chan model.AssetOption)
	doneCh := make(chan error)

	for range opts {
		go func() {
			var err error
			sleepDuration := 500 * time.Millisecond
			opt := <-assetsCh

			for tryCount := 0; tryCount < maxTryCount+1; tryCount++ {
				err = registerCustomExternal(opt)
				if err == nil {
					break
				}
				logger.WithNamespace("statik").
					Errorf("Could not load asset from %q, retrying in %s", opt.URL, sleepDuration)
				time.Sleep(sleepDuration)
				sleepDuration *= 4
			}

			doneCh <- err
		}()
	}

	for _, opt := range opts {
		assetsCh <- opt
	}
	close(assetsCh)

	var errm error
	for i := 0; i < len(opts); i++ {
		if err := <-doneCh; err != nil {
			errm = multierror.Append(errm, err)
		}
	}
	return errm
}

func registerCustomExternal(opt model.AssetOption) error {
	if opt.Context == "" {
		logger.WithNamespace("custom assets").
			Warnf("Could not load asset %s with empty context", opt.URL)
		return nil
	}

	opt.IsCustom = true

	assetURL := opt.URL

	var body io.Reader

	u, err := url.Parse(assetURL)
	if err != nil {
		return err
	}

	switch u.Scheme {
	case "http", "https":
		req, err := http.NewRequest(http.MethodGet, assetURL, nil)
		if err != nil {
			return err
		}
		res, err := assetsClient.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("could not load external asset on %s: status code %d", assetURL, res.StatusCode)
		}
		body = res.Body
	case "file":
		f, err := os.Open(u.Path)
		if err != nil {
			return err
		}
		defer f.Close()
		body = f
	default:
		return fmt.Errorf("does not support externals assets with scheme %q", u.Scheme)
	}

	h := sha256.New()
	brotliBuf := new(bytes.Buffer)
	bw := brotli.NewWriter(brotliBuf)

	teeReader := io.TeeReader(body, io.MultiWriter(h, bw))
	rawData, err := ioutil.ReadAll(teeReader)
	if err != nil {
		return err
	}
	if errc := bw.Close(); errc != nil {
		return errc
	}

	sum := h.Sum(nil)

	if opt.Shasum == "" {
		opt.Shasum = hex.EncodeToString(sum)
		log := logger.WithNamespace("custom_external")
		log.Warnf("shasum was not provided for file %s, inserting unsafe content %s: %s",
			opt.Name, opt.URL, opt.Shasum)
	}

	if hex.EncodeToString(sum) != opt.Shasum {
		return fmt.Errorf("external content checksum do not match: expected %s got %x on url %s",
			opt.Shasum, sum, assetURL)
	}

	asset := model.NewAsset(opt, rawData, brotliBuf.Bytes())

	err = assetFS.Add(asset.Context, asset.Name, asset)
	if err != nil {
		return err
	}

	return nil
}
