package deploy_test

import (
	"errors"
	"os"
	"testing"

	"github.com/spf13/afero"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"

	. "github.com/onsi/gomega"
)

const relatedImageEnvVar = "RELATED_IMAGE"

func TestParamsApplier_ApplyParams(t *testing.T) {
	testCases := []struct {
		name           string
		setupFS        func(afero.Fs)               // Setup filesystem state
		envGetter      func(string) string          // Mock environment variable getter
		componentPath  string                       // Input: component path
		file           string                       // Input: params file name
		imageParamsMap map[string]string            // Input: image params mapping
		extraParams    []map[string]string          // Input: extra params maps
		expectedError  bool                         // Expected: should return error
		errorContains  string                       // Expected: error message contains
		validateFS     func(*GomegaWithT, afero.Fs) // Validate filesystem state after
	}{
		{
			name: "updates params from env variables",
			setupFS: func(fs afero.Fs) {
				_ = fs.MkdirAll("/component", 0755)
				_ = afero.WriteFile(fs, "/component/params.env", []byte("IMAGE_A=old-value\n"), 0644)
			},
			envGetter: func(key string) string {
				if key == "RELATED_IMAGE_A" {
					return "new-value"
				}
				return ""
			},
			componentPath:  "/component",
			file:           "params.env",
			imageParamsMap: map[string]string{"IMAGE_A": "RELATED_IMAGE_A"},
			expectedError:  false,
			validateFS: func(g *GomegaWithT, fs afero.Fs) {
				content, err := afero.ReadFile(fs, "/component/params.env")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(string(content)).To(ContainSubstring("IMAGE_A=new-value"))
			},
		},
		{
			name: "updates params from extraParamsMaps",
			setupFS: func(fs afero.Fs) {
				_ = fs.MkdirAll("/component", 0755)
				_ = afero.WriteFile(fs, "/component/params.env", []byte("KEY1=value1\nKEY2=value2\n"), 0644)
			},
			componentPath: "/component",
			file:          "params.env",
			extraParams: []map[string]string{
				{"KEY1": "updated1"},
				{"KEY2": "updated2"},
			},
			expectedError: false,
			validateFS: func(g *GomegaWithT, fs afero.Fs) {
				content, err := afero.ReadFile(fs, "/component/params.env")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(string(content)).To(ContainSubstring("KEY1=updated1"))
				g.Expect(string(content)).To(ContainSubstring("KEY2=updated2"))
			},
		},
		{
			name: "returns nil if params.env does not exist",
			setupFS: func(fs afero.Fs) {
				_ = fs.Mkdir("/component", 0755)
			},
			componentPath: "/component",
			file:          "params.env",
			expectedError: false,
		},
		{
			name: "handles empty params.env file",
			setupFS: func(fs afero.Fs) {
				_ = fs.MkdirAll("/component", 0755)
				_ = afero.WriteFile(fs, "/component/params.env", []byte(""), 0644)
			},
			componentPath: "/component",
			file:          "params.env",
			expectedError: false,
		},
		{
			name: "combines env variables and extraParams",
			setupFS: func(fs afero.Fs) {
				_ = fs.MkdirAll("/component", 0755)
				_ = afero.WriteFile(fs, "/component/params.env", []byte("IMAGE=old\nCONFIG=old\n"), 0644)
			},
			envGetter: func(key string) string {
				if key == relatedImageEnvVar {
					return "new-image"
				}
				return ""
			},
			componentPath:  "/component",
			file:           "params.env",
			imageParamsMap: map[string]string{"IMAGE": relatedImageEnvVar},
			extraParams:    []map[string]string{{"CONFIG": "new-config"}},
			expectedError:  false,
			validateFS: func(g *GomegaWithT, fs afero.Fs) {
				content, err := afero.ReadFile(fs, "/component/params.env")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(string(content)).To(ContainSubstring("IMAGE=new-image"))
				g.Expect(string(content)).To(ContainSubstring("CONFIG=new-config"))
			},
		},
		{
			name: "imageParamsMap key not in params.env - does not add new key",
			setupFS: func(fs afero.Fs) {
				_ = fs.MkdirAll("/component", 0755)
				_ = afero.WriteFile(fs, "/component/params.env", []byte("EXISTING=value\n"), 0644)
			},
			envGetter: func(key string) string {
				// Only return value for relatedImageEnvVar, not for empty string
				if key == relatedImageEnvVar {
					return "new-value"
				}
				return ""
			},
			componentPath:  "/component",
			file:           "params.env",
			imageParamsMap: map[string]string{"NONEXISTENT": relatedImageEnvVar},
			expectedError:  false,
			validateFS: func(g *GomegaWithT, fs afero.Fs) {
				content, err := afero.ReadFile(fs, "/component/params.env")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(string(content)).To(Equal("EXISTING=value\n"))
				g.Expect(string(content)).NotTo(ContainSubstring("NONEXISTENT"))
			},
		},
		{
			name: "extraParams adds new key not in original file",
			setupFS: func(fs afero.Fs) {
				_ = fs.MkdirAll("/component", 0755)
				_ = afero.WriteFile(fs, "/component/params.env", []byte("EXISTING=value\n"), 0644)
			},
			componentPath: "/component",
			file:          "params.env",
			extraParams:   []map[string]string{{"NEW_KEY": "new-value"}},
			expectedError: false,
			validateFS: func(g *GomegaWithT, fs afero.Fs) {
				content, err := afero.ReadFile(fs, "/component/params.env")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(string(content)).To(ContainSubstring("EXISTING=value"))
				g.Expect(string(content)).To(ContainSubstring("NEW_KEY=new-value"))
			},
		},
		{
			name: "multiple extraParams maps - later overrides earlier",
			setupFS: func(fs afero.Fs) {
				_ = fs.MkdirAll("/component", 0755)
				_ = afero.WriteFile(fs, "/component/params.env", []byte("KEY=original\n"), 0644)
			},
			componentPath: "/component",
			file:          "params.env",
			extraParams: []map[string]string{
				{"KEY": "first"},
				{"KEY": "second"},
				{"KEY": "third"},
			},
			expectedError: false,
			validateFS: func(g *GomegaWithT, fs afero.Fs) {
				content, err := afero.ReadFile(fs, "/component/params.env")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(string(content)).To(ContainSubstring("KEY=third"))
			},
		},
		{
			name: "params.env with lines without equals sign - ignored",
			setupFS: func(fs afero.Fs) {
				_ = fs.MkdirAll("/component", 0755)
				_ = afero.WriteFile(fs, "/component/params.env", []byte("VALID=value\nINVALID_LINE\nANOTHER=test\n"), 0644)
			},
			componentPath: "/component",
			file:          "params.env",
			extraParams:   []map[string]string{{"VALID": "updated"}},
			expectedError: false,
			validateFS: func(g *GomegaWithT, fs afero.Fs) {
				content, err := afero.ReadFile(fs, "/component/params.env")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(string(content)).To(ContainSubstring("VALID=updated"))
				g.Expect(string(content)).To(ContainSubstring("ANOTHER=test"))
			},
		},
		{
			name: "value contains equals sign - preserves full value",
			setupFS: func(fs afero.Fs) {
				_ = fs.MkdirAll("/component", 0755)
				_ = afero.WriteFile(fs, "/component/params.env", []byte("URL=https://example.com?param=value\n"), 0644)
			},
			componentPath: "/component",
			file:          "params.env",
			expectedError: false,
			validateFS: func(g *GomegaWithT, fs afero.Fs) {
				content, err := afero.ReadFile(fs, "/component/params.env")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(string(content)).To(Equal("URL=https://example.com?param=value\n"))
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			memFs := afero.NewMemMapFs()

			if tc.setupFS != nil {
				tc.setupFS(memFs)
			}

			envGetter := tc.envGetter
			if envGetter == nil {
				envGetter = func(string) string { return "" }
			}

			applier := deploy.NewParamsApplier(
				deploy.WithFS(memFs),
				deploy.WithEnvGetter(envGetter),
			)

			err := applier.ApplyParams(tc.componentPath, tc.file, tc.imageParamsMap, tc.extraParams...)

			if tc.expectedError {
				g.Expect(err).To(HaveOccurred())
				if tc.errorContains != "" {
					g.Expect(err.Error()).To(ContainSubstring(tc.errorContains))
				}
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}

			if tc.validateFS != nil {
				tc.validateFS(g, memFs)
			}
		})
	}
}

func TestParamsApplier_ApplyParamsWithFallback(t *testing.T) {
	testCases := []struct {
		name           string
		setupFS        func(afero.Fs)               // Setup filesystem state
		envGetter      func(string) string          // Mock environment variable getter
		componentPath  string                       // Input: component path
		overlayName    string                       // Input: overlay name (odh, rhoai)
		imageParamsMap map[string]string            // Input: image params mapping
		extraParams    []map[string]string          // Input: extra params maps
		expectedPath   string                       // Expected: returned path
		expectedError  bool                         // Expected: should return error
		errorContains  string                       // Expected: error message contains
		validateFS     func(*GomegaWithT, afero.Fs) // Validate filesystem state after
	}{
		{
			name: "overlay exists - uses overlay",
			setupFS: func(fs afero.Fs) {
				_ = fs.MkdirAll("/component/overlays/odh", 0755)
				_ = fs.MkdirAll("/component/base", 0755)
				_ = afero.WriteFile(fs, "/component/overlays/odh/params.env", []byte("KEY=overlay\n"), 0644)
				_ = afero.WriteFile(fs, "/component/base/params.env", []byte("KEY=base\n"), 0644)
			},
			componentPath: "/component",
			overlayName:   "odh",
			expectedPath:  "/component/overlays/odh/params.env",
			expectedError: false,
		},
		{
			name: "overlay missing, base exists - uses base",
			setupFS: func(fs afero.Fs) {
				_ = fs.MkdirAll("/component/base", 0755)
				_ = afero.WriteFile(fs, "/component/base/params.env", []byte("KEY=base\n"), 0644)
			},
			componentPath: "/component",
			overlayName:   "odh",
			expectedPath:  "/component/base/params.env",
			expectedError: false,
		},
		{
			name: "both missing - returns error",
			setupFS: func(fs afero.Fs) {
				_ = fs.MkdirAll("/component", 0755)
			},
			componentPath: "/component",
			overlayName:   "odh",
			expectedError: true,
			errorContains: "params.env not found",
		},
		{
			name: "platform-specific params applied to overlay (odh)",
			setupFS: func(fs afero.Fs) {
				_ = fs.MkdirAll("/component/overlays/odh", 0755)
				_ = afero.WriteFile(fs, "/component/overlays/odh/params.env", []byte("PLATFORM_VERSION=old\n"), 0644)
			},
			componentPath: "/component",
			overlayName:   "odh",
			extraParams: []map[string]string{
				{"PLATFORM_VERSION": "2.0.0", "FIPS_ENABLED": "true"},
			},
			expectedPath:  "/component/overlays/odh/params.env",
			expectedError: false,
			validateFS: func(g *GomegaWithT, fs afero.Fs) {
				content, err := afero.ReadFile(fs, "/component/overlays/odh/params.env")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(string(content)).To(ContainSubstring("PLATFORM_VERSION=2.0.0"))
				g.Expect(string(content)).To(ContainSubstring("FIPS_ENABLED=true"))
			},
		},
		{
			name: "platform-specific params applied to overlay (rhoai)",
			setupFS: func(fs afero.Fs) {
				_ = fs.MkdirAll("/component/overlays/rhoai", 0755)
				_ = afero.WriteFile(fs, "/component/overlays/rhoai/params.env", []byte("PLATFORM_VERSION=old\n"), 0644)
			},
			componentPath: "/component",
			overlayName:   "rhoai",
			extraParams: []map[string]string{
				{"PLATFORM_VERSION": "2.0.0"},
			},
			expectedPath:  "/component/overlays/rhoai/params.env",
			expectedError: false,
			validateFS: func(g *GomegaWithT, fs afero.Fs) {
				content, err := afero.ReadFile(fs, "/component/overlays/rhoai/params.env")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(string(content)).To(ContainSubstring("PLATFORM_VERSION=2.0.0"))
			},
		},
		{
			name: "applies params to base when overlay missing",
			setupFS: func(fs afero.Fs) {
				_ = fs.MkdirAll("/component/base", 0755)
				_ = afero.WriteFile(fs, "/component/base/params.env", []byte("IMAGE=old\n"), 0644)
			},
			envGetter: func(key string) string {
				if key == "RELATED_IMAGE" {
					return "new-image"
				}
				return ""
			},
			componentPath:  "/component",
			overlayName:    "odh",
			imageParamsMap: map[string]string{"IMAGE": "RELATED_IMAGE"},
			expectedPath:   "/component/base/params.env",
			expectedError:  false,
			validateFS: func(g *GomegaWithT, fs afero.Fs) {
				content, err := afero.ReadFile(fs, "/component/base/params.env")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(string(content)).To(ContainSubstring("IMAGE=new-image"))
			},
		},
		{
			name: "overlay dir exists but params.env missing - falls back to base",
			setupFS: func(fs afero.Fs) {
				_ = fs.MkdirAll("/component/overlays/odh", 0755)
				_ = fs.MkdirAll("/component/base", 0755)
				_ = afero.WriteFile(fs, "/component/base/params.env", []byte("KEY=base\n"), 0644)
			},
			componentPath: "/component",
			overlayName:   "odh",
			expectedPath:  "/component/base/params.env",
			expectedError: false,
		},
		{
			name: "base dir exists but params.env missing - returns error",
			setupFS: func(fs afero.Fs) {
				_ = fs.MkdirAll("/component/base", 0755)
			},
			componentPath: "/component",
			overlayName:   "odh",
			expectedError: true,
			errorContains: "params.env not found",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			memFs := afero.NewMemMapFs()

			if tc.setupFS != nil {
				tc.setupFS(memFs)
			}

			envGetter := tc.envGetter
			if envGetter == nil {
				envGetter = func(string) string { return "" }
			}

			applier := deploy.NewParamsApplier(
				deploy.WithFS(memFs),
				deploy.WithEnvGetter(envGetter),
			)

			path, err := applier.ApplyParamsWithFallback(
				tc.componentPath, tc.overlayName, tc.imageParamsMap, tc.extraParams...)

			if tc.expectedError {
				g.Expect(err).To(HaveOccurred())
				if tc.errorContains != "" {
					g.Expect(err.Error()).To(ContainSubstring(tc.errorContains))
				}
			} else {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(path).To(Equal(tc.expectedPath))
			}

			if tc.validateFS != nil {
				tc.validateFS(g, memFs)
			}
		})
	}
}

// errorFs wraps an afero.Fs and returns errors for specific paths on Stat calls.
type errorFs struct {
	afero.Fs

	statErrorPath string
	statError     error
}

func (e *errorFs) Stat(name string) (os.FileInfo, error) {
	if name == e.statErrorPath {
		return nil, e.statError
	}
	return e.Fs.Stat(name)
}

// Separate tests for I/O error cases that require custom filesystem wrappers.
func TestApplyParamsWithFallback_OverlayStatIOError(t *testing.T) {
	g := NewWithT(t)

	baseFs := afero.NewMemMapFs()
	_ = baseFs.MkdirAll("/component/overlays/odh", 0755)
	_ = baseFs.MkdirAll("/component/base", 0755)
	_ = afero.WriteFile(baseFs, "/component/base/params.env", []byte("KEY=base\n"), 0644)

	errFs := &errorFs{
		Fs:            baseFs,
		statErrorPath: "/component/overlays/odh/params.env",
		statError:     errors.New("permission denied"),
	}

	applier := deploy.NewParamsApplier(deploy.WithFS(errFs))

	_, err := applier.ApplyParamsWithFallback("/component", "odh", nil)

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("failed to check overlay params file"))
}
