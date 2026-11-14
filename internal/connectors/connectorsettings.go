package connectors

import (
	"database/sql"
	"fmt"
	"slices"
	"strconv"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// ConnectorSetting is a declared setting with its current value (saved, else the default).
type ConnectorSetting struct {
	ManifestSetting
	Value string `json:"value"`
}

// check validates v against the setting's type.
func (st ManifestSetting) check(v string) error {
	switch st.Type {
	case "string":
	case "boolean":
		if v != "true" && v != "false" {
			return fmt.Errorf("%q is not true or false", v)
		}
	case "number":
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			return fmt.Errorf("%q is not a number", v)
		}
	case "enum":
		if !slices.Contains(st.Options, v) {
			return fmt.Errorf("%q is not one of %v", v, st.Options)
		}
	default:
		return fmt.Errorf("unknown type %q (have string, boolean, number, enum)", st.Type)
	}
	return nil
}

// ConnectorSettings lists the app's settings with current values; saved values that no longer
// validate (the manifest changed) fall back to the default.
func ConnectorSettings(db *sql.DB, name string) ([]ConnectorSetting, error) {
	a, err := store.GetConnector(db, name)
	if err != nil {
		return nil, err
	}
	m, err := ParseManifest(a.Manifest)
	if err != nil {
		return nil, err
	}
	saved, err := store.GetConnectorSettings(db, name)
	if err != nil {
		return nil, err
	}
	out := make([]ConnectorSetting, 0, len(m.Settings))
	for _, st := range m.Settings {
		v, ok := saved[st.Key]
		if !ok || st.check(v) != nil {
			v = fmt.Sprint(st.Default)
		}
		out = append(out, ConnectorSetting{st, v})
	}
	return out, nil
}

// SaveConnectorSettings validates and stores vals (key -> value) as the app's settings; unknown keys are an error.
func SaveConnectorSettings(db *sql.DB, name string, vals map[string]string) error {
	cur, err := ConnectorSettings(db, name)
	if err != nil {
		return err
	}
	for k := range vals {
		if !slices.ContainsFunc(cur, func(x ConnectorSetting) bool { return x.Key == k }) {
			return fmt.Errorf("app %q has no setting %q", name, k)
		}
	}
	for _, x := range cur {
		v, ok := vals[x.Key]
		if !ok {
			vals[x.Key] = x.Value
		} else if err := x.check(v); err != nil {
			return fmt.Errorf("setting %q: %w", x.Key, err)
		}
	}
	return store.SetConnectorSettings(db, name, vals)
}

// settingValues maps each setting key to its current value, for ${{ setting.key }}.
func settingValues(db *sql.DB, m Manifest, app string) (map[string]string, error) {
	if len(m.Settings) == 0 {
		return nil, nil
	}
	cur, err := ConnectorSettings(db, app)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, x := range cur {
		out[x.Key] = x.Value
	}
	return out, nil
}
