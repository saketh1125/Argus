package services

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// dockerOwnershipRecord is preparation for a durable lifecycle. It is not
// connected to the experimental adapter until reconciliation is implemented.
type dockerOwnershipRecord struct {
	RunID           string    `json:"run_id"`
	Name            string    `json:"name"`
	OwnershipDigest string    `json:"ownership_digest"`
	State           string    `json:"state"`
	CreatedAt       time.Time `json:"created_at"`
	ContainerID     string    `json:"container_id,omitempty"`
}

// Remains false until ownership transitions and recovery are implemented.
func dockerOwnershipLifecycleReady() bool { return false }

// validDockerOwnershipTransition defines the permitted lifecycle edges. A
// missing container is not evidence that an uncertain create was removed.
func validDockerOwnershipTransition(from, to string) bool {
	switch from {
	case "prepared":
		return to == "create_uncertain"
	case "create_uncertain":
		return to == "owned" || to == "ownership_conflict"
	case "owned":
		return to == "cleanup_pending" || to == "ownership_conflict"
	case "cleanup_pending":
		return to == "removed" || to == "ownership_conflict"
	default:
		return false
	}
}

// prepareDockerOwnership writes an exclusive, durable initial record. The
// caller must provide a private directory it controls; this primitive alone
// does not implement locking, transitions, or orphan recovery.
func prepareDockerOwnership(dir string) (dockerOwnershipRecord, error) {
	var record dockerOwnershipRecord
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return record, fmt.Errorf("ownership directory must be a private directory")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return record, err
	}
	defer root.Close()
	var id [16]byte
	var token [32]byte
	if _, err = rand.Read(id[:]); err != nil {
		return record, err
	}
	if _, err = rand.Read(token[:]); err != nil {
		return record, err
	}
	digest := sha256.Sum256(token[:])
	record = dockerOwnershipRecord{RunID: hex.EncodeToString(id[:]), OwnershipDigest: hex.EncodeToString(digest[:]), State: "prepared", CreatedAt: time.Now().UTC()}
	record.Name = "argus-experimental-" + record.RunID
	data, err := json.Marshal(record)
	if err != nil {
		return dockerOwnershipRecord{}, err
	}
	f, err := root.OpenFile(record.RunID+".json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return dockerOwnershipRecord{}, err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return dockerOwnershipRecord{}, err
	}
	d, err := os.Open(dir)
	if err != nil {
		return dockerOwnershipRecord{}, err
	}
	defer d.Close()
	if err = d.Sync(); err != nil {
		return dockerOwnershipRecord{}, err
	}
	return record, nil
}
