package web

import "os"

// Brand carries white-label settings so the same binary can be shipped as a
// differently-named control plane. All values come from BRAND_* env vars.
type Brand struct {
	Name    string // product name in the nav + <title>
	Logo    string // short mark inside the logo square
	Tagline string // small chip next to the name
	Status  string // text inside the mint status pill
}

// LoadBrand reads white-label settings from the environment, falling back to
// the default AI Engine identity.
func LoadBrand() Brand {
	return Brand{
		Name:    envOr("BRAND_NAME", "AI Engine Control Plane"),
		Logo:    envOr("BRAND_LOGO", "AI"),
		Tagline: envOr("BRAND_TAGLINE", "cloud·agnostic"),
		Status:  envOr("BRAND_STATUS", "Control Plane Active"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
