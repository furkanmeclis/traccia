package usecase

import "encoding/json"

// Recursive members are supplied by the closed recursive schema refiner rather
// than the inference library, which cannot infer recursive Go types.
func (f Filter) MarshalJSON() ([]byte, error) {
	type base Filter
	return json.Marshal(struct {
		base
		Children []Filter `json:"children,omitempty"`
		Child    *Filter  `json:"child,omitempty"`
	}{base(f), f.Children, f.Child})
}
func (f *Filter) UnmarshalJSON(b []byte) error {
	type base Filter
	v := struct {
		*base
		Children []Filter `json:"children,omitempty"`
		Child    *Filter  `json:"child,omitempty"`
	}{base: (*base)(f)}
	if e := json.Unmarshal(b, &v); e != nil {
		return e
	}
	f.Children = v.Children
	f.Child = v.Child
	return nil
}
