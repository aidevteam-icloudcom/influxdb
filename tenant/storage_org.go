package tenant

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/influxdata/influxdb/v2"
	"github.com/influxdata/influxdb/v2/kit/platform"
	"github.com/influxdata/influxdb/v2/kit/platform/errors"
	"github.com/influxdata/influxdb/v2/kv"
	errors2 "github.com/influxdata/influxdb/v2/pkg/errors"
)

var (
	organizationBucket = []byte("organizationsv1")
	organizationIndex  = []byte("organizationindexv1")
)

func (s *Store) uniqueOrgName(ctx context.Context, tx kv.Tx, uname string) error {
	key := organizationIndexKey(uname)
	if len(key) == 0 {
		return influxdb.ErrOrgNameisEmpty
	}

	idx, err := tx.Bucket(organizationIndex)

	if err != nil {
		return errors.ErrInternalServiceError(err)
	}

	_, err = idx.Get(key)
	// if not found then this is  _unique_.
	if kv.IsNotFound(err) {
		return nil
	}

	// no error means this is not unique
	if err == nil {
		return OrgAlreadyExistsError(uname)
	}

	// any other error is some sort of internal server error
	return errors.ErrInternalServiceError(err)
}

func organizationIndexKey(n string) []byte {
	return []byte(strings.TrimSpace(n))
}

func unmarshalOrg(v []byte) (*influxdb.Organization, error) {
	u := &influxdb.Organization{}
	if err := json.Unmarshal(v, u); err != nil {
		return nil, ErrCorruptOrg(err)
	}

	return u, nil
}

func marshalOrg(u *influxdb.Organization) ([]byte, error) {
	v, err := json.Marshal(u)
	if err != nil {
		return nil, ErrUnprocessableOrg(err)
	}

	return v, nil
}

func (s *Store) GetOrg(ctx context.Context, tx kv.Tx, id platform.ID) (*influxdb.Organization, error) {
	encodedID, err := id.Encode()
	if err != nil {
		return nil, InvalidOrgIDError(err)
	}

	b, err := tx.Bucket(organizationBucket)
	if err != nil {
		return nil, errors.ErrInternalServiceError(err, errors.WithErrorOp(influxdb.OpFindOrganizationByID))
	}

	v, err := b.Get(encodedID)
	if kv.IsNotFound(err) {
		return nil, ErrOrgNotFound
	}

	if err != nil {
		return nil, errors.ErrInternalServiceError(err, errors.WithErrorOp(influxdb.OpFindOrganizationByID))
	}

	return unmarshalOrg(v)
}

func (s *Store) GetOrgByName(ctx context.Context, tx kv.Tx, n string) (*influxdb.Organization, error) {
	b, err := tx.Bucket(organizationIndex)
	if err != nil {
		return nil, errors.ErrInternalServiceError(err, errors.WithErrorOp(influxdb.OpFindOrganization))
	}

	uid, err := b.Get(organizationIndexKey(n))
	if err == kv.ErrKeyNotFound {
		return nil, OrgNotFoundByName(n)
	}

	if err != nil {
		return nil, errors.ErrInternalServiceError(err, errors.WithErrorOp(influxdb.OpFindOrganization))
	}

	var id platform.ID
	if err := id.Decode(uid); err != nil {
		return nil, platform.ErrCorruptID(err)
	}
	return s.GetOrg(ctx, tx, id)
}

func (s *Store) ListOrgs(ctx context.Context, tx kv.Tx, opt ...influxdb.FindOptions) (orgs []*influxdb.Organization, retErr error) {
	// if we dont have any options it would be irresponsible to just give back all orgs in the system
	if len(opt) == 0 {
		opt = append(opt, influxdb.FindOptions{})
	}
	o := opt[0]

	b, err := tx.Bucket(organizationBucket)
	if err != nil {
		return nil, errors.ErrInternalServiceError(err, errors.WithErrorOp(influxdb.OpFindOrganizations))
	}

	cursor, err := b.ForwardCursor(nil)
	if err != nil {
		return nil, errors.ErrInternalServiceError(err, errors.WithErrorOp(influxdb.OpFindOrganizations))
	}
	defer errors2.Capture(&retErr,
		func() error {
			e := cursor.Close()
			return errors.ErrInternalServiceError(e, errors.WithErrorOp(influxdb.OpFindOrganizations))
		})()

	count := 0
	us := []*influxdb.Organization{}
	for k, v := cursor.Next(); k != nil; k, v = cursor.Next() {
		if o.Offset != 0 && count < o.Offset {
			count++
			continue
		}
		u, err := unmarshalOrg(v)
		if err != nil {
			continue
		}

		us = append(us, u)

		if o.Limit != 0 && len(us) >= o.Limit {
			break
		}
	}

	return us, errors.ErrInternalServiceError(cursor.Err(), errors.WithErrorOp(influxdb.OpFindOrganizations))
}

func (s *Store) CreateOrg(ctx context.Context, tx kv.Tx, o *influxdb.Organization) (err error) {
	// if ID is provided then ensure it is unique
	// generate new bucket ID
	o.ID, err = s.generateSafeID(ctx, tx, organizationBucket, s.OrgIDGen)
	if err != nil {
		return errors.ErrInternalServiceError(err, errors.WithErrorOp(influxdb.OpCreateOrganization))
	}

	encodedID, err := o.ID.Encode()
	if err != nil {
		return InvalidOrgIDError(err)
	}

	if err := s.uniqueOrgName(ctx, tx, o.Name); err != nil {
		return err
	}

	o.SetCreatedAt(s.now())
	o.SetUpdatedAt(s.now())
	idx, err := tx.Bucket(organizationIndex)
	if err != nil {
		return errors.ErrInternalServiceError(err, errors.WithErrorOp(influxdb.OpCreateOrganization))
	}

	b, err := tx.Bucket(organizationBucket)
	if err != nil {
		return errors.ErrInternalServiceError(err, errors.WithErrorOp(influxdb.OpCreateOrganization))
	}

	v, err := marshalOrg(o)
	if err != nil {
		return errors.ErrInternalServiceError(err, errors.WithErrorOp(influxdb.OpCreateOrganization))
	}

	if err := idx.Put(organizationIndexKey(o.Name), encodedID); err != nil {
		return errors.ErrInternalServiceError(err, errors.WithErrorOp(influxdb.OpCreateOrganization))
	}

	if err := b.Put(encodedID, v); err != nil {
		return errors.ErrInternalServiceError(err, errors.WithErrorOp(influxdb.OpCreateOrganization))
	}

	return nil
}

func (s *Store) UpdateOrg(ctx context.Context, tx kv.Tx, id platform.ID, upd influxdb.OrganizationUpdate) (*influxdb.Organization, error) {
	encodedID, err := id.Encode()
	if err != nil {
		return nil, err
	}

	u, err := s.GetOrg(ctx, tx, id)
	if err != nil {
		return nil, err
	}

	u.SetUpdatedAt(s.now())
	if upd.Name != nil && u.Name != *upd.Name {
		if err := s.uniqueOrgName(ctx, tx, *upd.Name); err != nil {
			return nil, err
		}

		idx, err := tx.Bucket(organizationIndex)
		if err != nil {
			return nil, errors.ErrInternalServiceError(err, errors.WithErrorOp(influxdb.OpUpdateOrganization))
		}

		if err := idx.Delete(organizationIndexKey(u.Name)); err != nil {
			return nil, errors.ErrInternalServiceError(err, errors.WithErrorOp(influxdb.OpUpdateOrganization))
		}

		u.Name = *upd.Name

		if err := idx.Put(organizationIndexKey(*upd.Name), encodedID); err != nil {
			return nil, errors.ErrInternalServiceError(err, errors.WithErrorOp(influxdb.OpUpdateOrganization))
		}
	}

	if upd.Description != nil {
		u.Description = *upd.Description
	}

	v, err := marshalOrg(u)
	if err != nil {
		return nil, err
	}

	b, err := tx.Bucket(organizationBucket)
	if err != nil {
		return nil, errors.ErrInternalServiceError(err, errors.WithErrorOp(influxdb.OpUpdateOrganization))
	}
	if err := b.Put(encodedID, v); err != nil {
		return nil, errors.ErrInternalServiceError(err, errors.WithErrorOp(influxdb.OpUpdateOrganization))
	}

	return u, nil
}

func (s *Store) DeleteOrg(ctx context.Context, tx kv.Tx, id platform.ID) error {
	u, err := s.GetOrg(ctx, tx, id)
	if err != nil {
		return err
	}

	encodedID, err := id.Encode()
	if err != nil {
		return InvalidOrgIDError(err)
	}

	idx, err := tx.Bucket(organizationIndex)
	if err != nil {
		return errors.ErrInternalServiceError(err, errors.WithErrorOp(influxdb.OpDeleteOrganization))
	}

	if err := idx.Delete([]byte(u.Name)); err != nil {
		return errors.ErrInternalServiceError(err, errors.WithErrorOp(influxdb.OpDeleteOrganization))
	}

	b, err := tx.Bucket(organizationBucket)
	if err != nil {
		return errors.ErrInternalServiceError(err, errors.WithErrorOp(influxdb.OpDeleteOrganization))
	}

	if err := b.Delete(encodedID); err != nil {
		return errors.ErrInternalServiceError(err, errors.WithErrorOp(influxdb.OpDeleteOrganization))
	}

	return nil
}
