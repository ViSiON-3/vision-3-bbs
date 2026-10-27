package user

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

// APIDevice is a phone paired with the QWK API. The device holds the private
// key; the BBS keeps only the public half and checks signatures against it.
type APIDevice struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	PublicKey string `json:"publicKey"` // base64 PKIX DER, P-256 ECDSA or Ed25519
	// PasswordStamp ties the device to the password it was paired under. A
	// device paired by someone holding a stolen password must not outlive the
	// owner changing it, so a stamp that no longer matches the account's
	// password makes the device invalid, whoever changed the password and
	// however (terminal, sysop editors, ./ue).
	PasswordStamp string    `json:"passwordStamp"`
	CreatedAt     time.Time `json:"createdAt"`
	LastUsed      time.Time `json:"lastUsed,omitempty"`
}

var (
	// ErrTooManyDevices is returned when pairing would exceed the per-user cap.
	ErrTooManyDevices = errors.New("too many paired devices")
	// ErrDeviceKeyExists is returned when the key is already paired to the user.
	ErrDeviceKeyExists = errors.New("device key already paired")
	// ErrPasswordChanged is returned when the password the pairing was
	// authenticated under is no longer the account's password.
	ErrPasswordChanged = errors.New("password changed since the pairing was authenticated")
)

// PasswordStamp derives the stamp recorded on a device from a password hash.
// It is a digest rather than the hash itself, so the device list never holds
// a second copy of the bcrypt hash.
func PasswordStamp(passwordHash string) string {
	sum := sha256.Sum256([]byte("vision3-api-device\x00" + passwordHash))
	return hex.EncodeToString(sum[:16])
}

// ValidAPIDevices returns the devices still paired under the current password.
func (u *User) ValidAPIDevices() []APIDevice {
	stamp := PasswordStamp(u.PasswordHash)
	var out []APIDevice
	for _, d := range u.APIDevices {
		if d.PasswordStamp == stamp {
			out = append(out, d)
		}
	}
	return out
}

// APIDevice returns the valid device with the given ID.
func (u *User) APIDevice(id string) (APIDevice, bool) {
	for _, d := range u.ValidAPIDevices() {
		if d.ID == id {
			return d, true
		}
	}
	return APIDevice{}, false
}

// AddAPIDevice pairs d with the user. The caller sets d.PasswordStamp to the
// stamp of the password the pairing was authenticated with; if that is no
// longer the account's password the pairing fails with ErrPasswordChanged.
// Checking here, under the manager lock, matters: stamping with whatever the
// password is at save time would let someone holding a stolen password start
// a pairing, wait out the owner's password reset, and finish with a device
// stamped by the new password, which the reset would then not void.
//
// Devices left over from an earlier password are dropped as it is saved. It
// fails with ErrTooManyDevices once the user has max valid devices, and with
// ErrDeviceKeyExists if the same key is already paired.
func (um *UserMgr) AddAPIDevice(userID int, d APIDevice, max int) error {
	return um.mutateAPIDevices(userID, func(u *User, valid []APIDevice) ([]APIDevice, error) {
		if d.PasswordStamp != PasswordStamp(u.PasswordHash) {
			return nil, ErrPasswordChanged
		}
		for _, existing := range valid {
			if existing.PublicKey == d.PublicKey {
				return nil, ErrDeviceKeyExists
			}
		}
		if len(valid) >= max {
			return nil, ErrTooManyDevices
		}
		return append(valid, d), nil
	})
}

// RemoveAPIDevice unpairs a device, reporting whether it was paired.
func (um *UserMgr) RemoveAPIDevice(userID int, deviceID string) (bool, error) {
	removed := false
	err := um.mutateAPIDevices(userID, func(_ *User, valid []APIDevice) ([]APIDevice, error) {
		out := make([]APIDevice, 0, len(valid))
		for _, d := range valid {
			if d.ID == deviceID {
				removed = true
				continue
			}
			out = append(out, d)
		}
		return out, nil
	})
	return removed, err
}

// TouchAPIDevice records a login by the device.
func (um *UserMgr) TouchAPIDevice(userID int, deviceID string, at time.Time) error {
	return um.mutateAPIDevices(userID, func(_ *User, valid []APIDevice) ([]APIDevice, error) {
		out := make([]APIDevice, len(valid))
		copy(out, valid)
		for i := range out {
			if out[i].ID == deviceID {
				out[i].LastUsed = at
			}
		}
		return out, nil
	})
}

// FindAPIDevice returns the user a valid device is paired with, and the device.
func (um *UserMgr) FindAPIDevice(deviceID string) (*User, APIDevice, bool) {
	um.mu.RLock()
	defer um.mu.RUnlock()
	for _, u := range um.users {
		if d, ok := u.APIDevice(deviceID); ok {
			userCopy := *u
			userCopy.TwitList = append([]TwitEntry(nil), u.TwitList...)
			return &userCopy, d, true
		}
	}
	return nil, APIDevice{}, false
}

// mutateAPIDevices replaces the user's device list with what fn returns from
// the currently valid devices, under the manager lock, and saves. Working on
// the stored record directly, rather than on a caller's copy through
// UpdateUser, means a pairing cannot write back stale session fields.
func (um *UserMgr) mutateAPIDevices(userID int, fn func(u *User, valid []APIDevice) ([]APIDevice, error)) error {
	um.mu.Lock()
	defer um.mu.Unlock()
	var target *User
	for _, u := range um.users {
		if u.ID == userID {
			target = u
			break
		}
	}
	if target == nil || target.DeletedUser {
		return ErrUserNotFound
	}
	next, err := fn(target, target.ValidAPIDevices())
	if err != nil {
		return err
	}
	previous := target.APIDevices
	target.APIDevices = next
	if err := um.saveUsersLocked(); err != nil {
		target.APIDevices = previous
		return err
	}
	return nil
}
