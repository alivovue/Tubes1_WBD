package dbmanagement

import (
	"context"
	"net"
	"strings"

	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/database/dao"
)

func (s *Service) requireUniqueConnection(ctx context.Context, store *dao.Store, id int64, input ConnectionInput) error {
	databases, err := store.Databases(ctx)
	if err != nil {
		return err
	}
	host := connectionHost(input.Host)
	candidates := make([]dao.Database, 0)
	for _, database := range databases {
		if database.ID == id || adapters.Normalize(database.DBMS) != input.DBMS || database.Port != input.Port || database.DatabaseName != input.DatabaseName {
			continue
		}
		if connectionHost(database.Host) == host {
			return ErrDuplicateConnection
		}
		candidates = append(candidates, database)
	}
	if len(candidates) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.ConnectTimeout)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return ErrConnection
	}
	for _, database := range candidates {
		existing, err := net.DefaultResolver.LookupIPAddr(ctx, connectionHost(database.Host))
		if err != nil {
			return ErrConnection
		}
		for _, address := range addresses {
			for _, other := range existing {
				if address.IP.Equal(other.IP) && address.Zone == other.Zone {
					return ErrDuplicateConnection
				}
			}
		}
	}
	return nil
}

func connectionHost(host string) string {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if address := net.ParseIP(host); address != nil {
		return address.String()
	}
	return host
}
