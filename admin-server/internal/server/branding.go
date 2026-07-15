package server

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const maxBrandingAsset = 2 << 20

type brandingAsset struct {
	Filename string
	Kind     string
}

var brandingAssets = map[string]brandingAsset{
	"wordmark": {Filename: "searxng-wordmark.svg", Kind: "svg"},
	"logo":     {Filename: "searxng.png", Kind: "png"},
	"favicon":  {Filename: "favicon.png", Kind: "png"},
	"icon192":  {Filename: "192.png", Kind: "png"},
	"icon512":  {Filename: "512.png", Kind: "png"},
}

func (handler *Handler) serveBranding(response http.ResponseWriter, request *http.Request, assetName string) {
	if request.Method != http.MethodPost {
		methodNotAllowed(response, http.MethodPost)
		return
	}
	asset, ok := brandingAssets[assetName]
	if !ok {
		http.NotFound(response, request)
		return
	}
	handler.saveMu.Lock()
	defer handler.saveMu.Unlock()
	data, err := readUpload(request)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errUploadTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		writeJSON(response, status, map[string]string{"error": err.Error()})
		return
	}
	if err := validateAsset(asset.Kind, data); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	assetPath := filepath.Join(handler.brandingDir, asset.Filename)
	previous, previousErr := os.ReadFile(assetPath)
	if previousErr != nil && !errors.Is(previousErr, os.ErrNotExist) {
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "read current branding asset: " + previousErr.Error()})
		return
	}
	if err := atomicWrite(assetPath, data, 0o644); err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "save branding asset: " + err.Error()})
		return
	}
	result := handler.applyBrandingAsset(assetPath, previous, previousErr == nil)
	writeJSON(response, http.StatusOK, map[string]any{
		"saved": !result.RolledBack, "asset": assetName, "filename": asset.Filename,
		"applied": result.Applied, "restartRequired": result.RestartRequired, "rolledBack": result.RolledBack, "warnings": result.Warnings,
	})
}

var errUploadTooLarge = errors.New("branding asset exceeds 2 MiB")

func readUpload(request *http.Request) ([]byte, error) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err == nil && mediaType == "multipart/form-data" {
		reader, err := request.MultipartReader()
		if err != nil {
			return nil, errors.New("invalid multipart upload")
		}
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				return nil, errors.New("multipart upload has no file")
			}
			if err != nil {
				return nil, errors.New("invalid multipart upload")
			}
			if part.FileName() == "" {
				part.Close()
				continue
			}
			data, err := readLimited(part, maxBrandingAsset)
			part.Close()
			return data, err
		}
	}
	return readLimited(request.Body, maxBrandingAsset)
}

func readLimited(reader io.Reader, maximum int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, fmt.Errorf("read upload: %w", err)
	}
	if int64(len(data)) > maximum {
		return nil, errUploadTooLarge
	}
	if len(data) == 0 {
		return nil, errors.New("branding asset is empty")
	}
	return data, nil
}

func validateAsset(kind string, data []byte) error {
	switch kind {
	case "png":
		if !bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
			return errors.New("asset must be a PNG file")
		}
	case "svg":
		decoder := xml.NewDecoder(bytes.NewReader(data))
		for {
			token, err := decoder.Token()
			if err != nil {
				return errors.New("asset must be a valid SVG file")
			}
			switch value := token.(type) {
			case xml.Directive:
				if strings.Contains(strings.ToUpper(string(value)), "DOCTYPE") {
					return errors.New("SVG DOCTYPE is not allowed")
				}
			case xml.StartElement:
				if value.Name.Local != "svg" {
					return errors.New("asset root element must be svg")
				}
				return nil
			}
		}
	default:
		return errors.New("unsupported branding asset type")
	}
	return nil
}
