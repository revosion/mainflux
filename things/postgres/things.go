// Copyright (c) Mainflux
// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/gofrs/uuid"
	"github.com/lib/pq" // required for DB access
	"github.com/mainflux/mainflux/things"
)

const (
	errDuplicate  = "unique_violation"
	errFK         = "foreign_key_violation"
	errInvalid    = "invalid_text_representation"
	errTruncation = "string_data_right_truncation"
)

var _ things.ThingRepository = (*thingRepository)(nil)

type thingRepository struct {
	db Database
}

// NewThingRepository instantiates a PostgreSQL implementation of thing
// repository.
func NewThingRepository(db Database) things.ThingRepository {
	return &thingRepository{
		db: db,
	}
}

func (tr thingRepository) Save(ctx context.Context, thing things.Thing) (string, error) {
	q := `INSERT INTO things (id, owner, name, key, metadata)
		  VALUES (:id, :owner, :name, :key, :metadata);`

	dbth, err := toDBThing(thing)
	if err != nil {
		return "", err
	}

	_, err = tr.db.NamedExecContext(ctx, q, dbth)
	if err != nil {
		pqErr, ok := err.(*pq.Error)
		if ok {
			switch pqErr.Code.Name() {
			case errInvalid, errTruncation:
				return "", things.ErrMalformedEntity
			case errDuplicate:
				return "", things.ErrConflict
			}
		}

		return "", err
	}

	return dbth.ID, nil
}

func (tr thingRepository) Update(ctx context.Context, thing things.Thing) error {
	q := `UPDATE things SET name = :name, metadata = :metadata WHERE owner = :owner AND id = :id;`

	dbth, err := toDBThing(thing)
	if err != nil {
		return err
	}

	res, err := tr.db.NamedExecContext(ctx, q, dbth)
	if err != nil {
		pqErr, ok := err.(*pq.Error)
		if ok {
			switch pqErr.Code.Name() {
			case errInvalid, errTruncation:
				return things.ErrMalformedEntity
			}
		}

		return err
	}

	cnt, err := res.RowsAffected()
	if err != nil {
		return err
	}

	if cnt == 0 {
		return things.ErrNotFound
	}

	return nil
}

func (tr thingRepository) UpdateKey(ctx context.Context, owner, id, key string) error {
	q := `UPDATE things SET key = :key WHERE owner = :owner AND id = :id;`

	dbth := dbThing{
		ID:    id,
		Owner: owner,
		Key:   key,
	}

	res, err := tr.db.NamedExecContext(ctx, q, dbth)
	if err != nil {
		pqErr, ok := err.(*pq.Error)
		if ok {
			switch pqErr.Code.Name() {
			case errInvalid:
				return things.ErrMalformedEntity
			case errDuplicate:
				return things.ErrConflict
			}
		}

		return err
	}

	cnt, err := res.RowsAffected()
	if err != nil {
		return err
	}

	if cnt == 0 {
		return things.ErrNotFound
	}

	return nil
}

func (tr thingRepository) RetrieveByID(ctx context.Context, owner, id string) (things.Thing, error) {
	q := `SELECT name, key, metadata FROM things WHERE id = $1 AND owner = $2;`

	dbth := dbThing{
		ID:    id,
		Owner: owner,
	}

	if err := tr.db.QueryRowxContext(ctx, q, id, owner).StructScan(&dbth); err != nil {
		empty := things.Thing{}

		pqErr, ok := err.(*pq.Error)
		if err == sql.ErrNoRows || ok && errInvalid == pqErr.Code.Name() {
			return empty, things.ErrNotFound
		}

		return empty, err
	}

	return toThing(dbth)
}

func (tr thingRepository) RetrieveByKey(ctx context.Context, key string) (string, error) {
	q := `SELECT id FROM things WHERE key = $1;`

	var id string
	if err := tr.db.QueryRowxContext(ctx, q, key).Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return "", things.ErrNotFound
		}
		return "", err
	}

	return id, nil
}

func (tr thingRepository) RetrieveAll(ctx context.Context, owner string, offset, limit uint64, name string, metadata things.Metadata) (things.ThingsPage, error) {
	nq, name := getNameQuery(name)
	m, mq, err := getMetadataQuery(metadata)
	if err != nil {
		return things.ThingsPage{}, err
	}

	q := fmt.Sprintf(`SELECT id, name, key, metadata FROM things
		  WHERE owner = :owner %s%s ORDER BY id LIMIT :limit OFFSET :offset;`, mq, nq)

	params := map[string]interface{}{
		"owner":    owner,
		"limit":    limit,
		"offset":   offset,
		"name":     name,
		"metadata": m,
	}

	rows, err := tr.db.NamedQueryContext(ctx, q, params)
	if err != nil {
		return things.ThingsPage{}, err
	}
	defer rows.Close()

	items := []things.Thing{}
	for rows.Next() {
		dbth := dbThing{Owner: owner}
		if err := rows.StructScan(&dbth); err != nil {
			return things.ThingsPage{}, err
		}

		th, err := toThing(dbth)
		if err != nil {
			return things.ThingsPage{}, err
		}

		items = append(items, th)
	}

	cq := ""
	if name != "" {
		cq = `AND LOWER(name) LIKE $2`
	}

	q = fmt.Sprintf(`SELECT COUNT(*) FROM things WHERE owner = $1 %s;`, cq)

	total := uint64(0)
	switch name {
	case "":
		if err := tr.db.GetContext(ctx, &total, q, owner); err != nil {
			return things.ThingsPage{}, err
		}
	default:
		if err := tr.db.GetContext(ctx, &total, q, owner, name); err != nil {
			return things.ThingsPage{}, err
		}
	}

	page := things.ThingsPage{
		Things: items,
		PageMetadata: things.PageMetadata{
			Total:  total,
			Offset: offset,
			Limit:  limit,
		},
	}

	return page, nil
}

func (tr thingRepository) RetrieveByChannel(ctx context.Context, owner, channel string, offset, limit uint64) (things.ThingsPage, error) {
	// Verify if UUID format is valid to avoid internal Postgres error
	if _, err := uuid.FromString(channel); err != nil {
		return things.ThingsPage{}, things.ErrNotFound
	}

	q := `SELECT id, name, key, metadata
	      FROM things th
	      INNER JOIN connections co
		  ON th.id = co.thing_id
		  WHERE th.owner = :owner AND co.channel_id = :channel
		  ORDER BY th.id
		  LIMIT :limit
		  OFFSET :offset;`

	params := map[string]interface{}{
		"owner":   owner,
		"channel": channel,
		"limit":   limit,
		"offset":  offset,
	}

	rows, err := tr.db.NamedQueryContext(ctx, q, params)
	if err != nil {
		return things.ThingsPage{}, err
	}
	defer rows.Close()

	items := []things.Thing{}
	for rows.Next() {
		dbth := dbThing{Owner: owner}
		if err := rows.StructScan(&dbth); err != nil {
			return things.ThingsPage{}, err
		}

		th, err := toThing(dbth)
		if err != nil {
			return things.ThingsPage{}, err
		}

		items = append(items, th)
	}

	q = `SELECT COUNT(*)
	     FROM things th
	     INNER JOIN connections co
	     ON th.id = co.thing_id
	     WHERE th.owner = $1 AND co.channel_id = $2;`

	var total uint64
	if err := tr.db.GetContext(ctx, &total, q, owner, channel); err != nil {
		return things.ThingsPage{}, err
	}

	return things.ThingsPage{
		Things: items,
		PageMetadata: things.PageMetadata{
			Total:  total,
			Offset: offset,
			Limit:  limit,
		},
	}, nil
}

func (tr thingRepository) Remove(ctx context.Context, owner, id string) error {
	dbth := dbThing{
		ID:    id,
		Owner: owner,
	}
	q := `DELETE FROM things WHERE id = :id AND owner = :owner;`
	tr.db.NamedExecContext(ctx, q, dbth)
	return nil
}

type dbThing struct {
	ID       string `db:"id"`
	Owner    string `db:"owner"`
	Name     string `db:"name"`
	Key      string `db:"key"`
	Metadata []byte `db:"metadata"`
}

func toDBThing(th things.Thing) (dbThing, error) {
	data := []byte("{}")
	if len(th.Metadata) > 0 {
		b, err := json.Marshal(th.Metadata)
		if err != nil {
			return dbThing{}, err
		}
		data = b
	}

	return dbThing{
		ID:       th.ID,
		Owner:    th.Owner,
		Name:     th.Name,
		Key:      th.Key,
		Metadata: data,
	}, nil
}

func toThing(dbth dbThing) (things.Thing, error) {
	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(dbth.Metadata), &metadata); err != nil {
		return things.Thing{}, err
	}

	return things.Thing{
		ID:       dbth.ID,
		Owner:    dbth.Owner,
		Name:     dbth.Name,
		Key:      dbth.Key,
		Metadata: metadata,
	}, nil
}
