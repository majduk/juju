package state

import (
	"fmt"
	"launchpad.net/juju-core/charm"
)

// RelationRole defines the role of a relation endpoint.
type RelationRole string

const (
	RoleProvider RelationRole = "provider"
	RoleRequirer RelationRole = "requirer"
	RolePeer     RelationRole = "peer"
)

// counterpartRole returns the RelationRole that this RelationRole
// can relate to.
// This should remain an internal method because the relation
// model does not guarantee that for every role there will
// necessarily exist a single counterpart role that is sensible
// for basing algorithms upon.
func (r RelationRole) counterpartRole() RelationRole {
	switch r {
	case RoleProvider:
		return RoleRequirer
	case RoleRequirer:
		return RoleProvider
	case RolePeer:
		return RolePeer
	}
	panic(fmt.Errorf("unknown relation role %q", r))
}

// Endpoint represents one endpoint of a relation.
type Endpoint struct {
	ServiceName   string
	Interface     string
	RelationName  string
	RelationRole  RelationRole
	RelationScope charm.RelationScope
}

// String returns the unique identifier of the relation endpoint.
func (ep Endpoint) String() string {
	return ep.ServiceName + ":" + ep.RelationName
}

// CanRelateTo returns whether a relation may be established between e and other.
func (ep Endpoint) CanRelateTo(other Endpoint) bool {
	if ep.ServiceName == other.ServiceName {
		return false
	}
	if ep.Interface != other.Interface {
		return false
	}
	if ep.RelationRole == RolePeer {
		// Peer relations do not currently work with multiple endpoints.
		return false
	}
	return ep.RelationRole.counterpartRole() == other.RelationRole
}

// ImplementedBy returns whether the endpoint is implemented by the supplied charm.
func (ep Endpoint) ImplementedBy(ch charm.Charm) bool {
	if ep.isImplicit() {
		return true
	}
	var m map[string]charm.Relation
	switch ep.RelationRole {
	case RoleProvider:
		m = ch.Meta().Provides
	case RoleRequirer:
		m = ch.Meta().Requires
	case RolePeer:
		m = ch.Meta().Peers
	default:
		panic(fmt.Errorf("unknown relation role %q", ep.RelationRole))
	}
	rel, found := m[ep.RelationName]
	if !found {
		return false
	}
	if rel.Interface == ep.Interface {
		switch ep.RelationScope {
		case charm.ScopeGlobal:
			return rel.Scope != charm.ScopeContainer
		case charm.ScopeContainer:
			return true
		default:
			panic(fmt.Errorf("unknown relation scope %q", ep.RelationScope))
		}
	}
	return false
}

// isImplicit returns whether the endpoint is supplied by juju itself,
// rather than by a charm.
func (ep Endpoint) isImplicit() bool {
	if ep.RelationName != "juju-info" {
		return false
	} else if ep.Interface != "juju-info" {
		return false
	} else if ep.RelationRole != RoleProvider {
		return false
	}
	return true
}
