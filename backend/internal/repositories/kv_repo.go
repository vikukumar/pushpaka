package repositories

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/dgraph-io/badger/v4"
)

var ErrKeyNotFound = errors.New("key not found in kv store")

type KVRepository struct {
	db *badger.DB
}

func NewKVRepository(path string) (*KVRepository, error) {
	opts := badger.DefaultOptions(path)
	// Optimize for fast sync and minimal overhead
	opts.Logger = nil // Disable verbose logging
	opts.SyncWrites = false // We batch sync to postgres later, so async writes are fine here

	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}

	return &KVRepository{db: db}, nil
}

func (r *KVRepository) Close() error {
	if r.db != nil {
		return r.db.Close()
	}
	return nil
}

// Set stores a value as JSON with an optional TTL
func (r *KVRepository) Set(key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return r.db.Update(func(txn *badger.Txn) error {
		e := badger.NewEntry([]byte(key), data)
		if ttl > 0 {
			e.WithTTL(ttl)
		}
		return txn.SetEntry(e)
	})
}

// Get retrieves and unmarshals a JSON value
func (r *KVRepository) Get(key string, dest interface{}) error {
	return r.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return ErrKeyNotFound
			}
			return err
		}

		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, dest)
		})
	})
}

// Delete removes a key
func (r *KVRepository) Delete(key string) error {
	return r.db.Update(func(txn *badger.Txn) error {
		err := txn.Delete([]byte(key))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil // Idempotent
		}
		return err
	})
}

// SetRaw stores raw bytes
func (r *KVRepository) SetRaw(key string, data []byte, ttl time.Duration) error {
	return r.db.Update(func(txn *badger.Txn) error {
		e := badger.NewEntry([]byte(key), data)
		if ttl > 0 {
			e.WithTTL(ttl)
		}
		return txn.SetEntry(e)
	})
}

// GetRaw retrieves raw bytes
func (r *KVRepository) GetRaw(key string) ([]byte, error) {
	var data []byte
	err := r.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return ErrKeyNotFound
			}
			return err
		}
		valCopy, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}
		data = valCopy
		return nil
	})
	return data, err
}
