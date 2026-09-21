package store

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
)

// DeviceLabelMax is how long a phone-reported label may be. A UA string
// is not a novel; Settings only needs a model line.
const DeviceLabelMax = 80

// RemoteDevice is what this PC remembers about a bound phone after it
// has connected. The hub only stores a fingerprint; the label is
// reported by the phone so Settings can show a model instead of hex.
type RemoteDevice struct {
	DeviceFP string    `gorm:"primaryKey;size:32" json:"device_fp"`
	Label    string    `gorm:"size:120" json:"label"`
	SeenAt   time.Time `json:"seen_at"`
}

// TouchRemoteDevice records that this fingerprint just linked. An empty
// label must not wipe one the phone already sent — reconnects arrive
// before hello.
func (s *Store) TouchRemoteDevice(fp, label string) error {
	if s == nil {
		return nil
	}
	fp = strings.TrimSpace(fp)
	if fp == "" {
		return nil
	}
	label = ClipDeviceLabel(label)
	now := time.Now().UTC()
	var row RemoteDevice
	err := s.db.Where("device_fp = ?", fp).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.db.Create(&RemoteDevice{DeviceFP: fp, Label: label, SeenAt: now}).Error
	}
	if err != nil {
		return err
	}
	row.SeenAt = now
	if label != "" {
		row.Label = label
	}
	return s.db.Save(&row).Error
}

func (s *Store) ListRemoteDevices() ([]RemoteDevice, error) {
	if s == nil {
		return nil, nil
	}
	var rows []RemoteDevice
	if err := s.db.Find(&rows).Error; err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []RemoteDevice{}
	}
	return rows, nil
}

func (s *Store) RemoteDevice(fp string) (*RemoteDevice, error) {
	if s == nil {
		return nil, ErrNotFound
	}
	fp = strings.TrimSpace(fp)
	if fp == "" {
		return nil, ErrNotFound
	}
	var row RemoteDevice
	err := s.db.Where("device_fp = ?", fp).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// ClipDeviceLabel strips control junk and caps length so a hostile UA
// cannot write a manifesto into Settings.
func ClipDeviceLabel(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if r == '\u0000' || (unicode.IsControl(r) && r != '\t') {
			if !prevSpace && b.Len() > 0 {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		if unicode.IsSpace(r) {
			if !prevSpace && b.Len() > 0 {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	out := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(out) <= DeviceLabelMax {
		return out
	}
	runes := []rune(out)
	return string(runes[:DeviceLabelMax])
}
