package cli

import (
	"strconv"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	clouddnsv1 "go.anx.io/go-anxcloud/pkg/apis/clouddns/v1"

	"github.com/ProbstenHias/anexia-cli/internal/errmap"
	"github.com/ProbstenHias/anexia-cli/internal/resource"
)

// newDNSCommand builds the "dns" group, covering the Engine's CloudDNS area:
// the zones you own and the records inside them.
func newDNSCommand(opts *globalOptions) *cobra.Command {
	return resource.Group("dns", "CloudDNS zones and records",
		newDNSZoneCommand(opts),
		newDNSRecordCommand(opts),
	)
}

// SOA defaults the CLI sends when a zone is created without them. The Engine
// requires all four, so leaving them at Go's zero value would produce a zone
// that refreshes and expires immediately rather than one the caller can fix
// later.
const (
	defaultRefresh = 3600
	defaultRetry   = 900
	defaultExpire  = 604800
	defaultTTL     = 3600
)

// newDNSZoneCommand builds "dns zone". The generic client covers the whole
// lifecycle, so only the two operations with no CRUD spelling, import and
// apply, are written by hand.
func newDNSZoneCommand(opts *globalOptions) *cobra.Command {
	cmd := resource.Command(opts, resource.Spec[clouddnsv1.Zone, *clouddnsv1.Zone]{
		Noun:   "zone",
		Short:  "Work with CloudDNS zones",
		List:   true,
		Get:    true,
		Delete: true,
		// A zone is addressed by its name, not by a separate identifier.
		Identify: func(z *clouddnsv1.Zone, id string) {
			z.Name = id
		},
		CreatePayload: zoneCreateFlags,
		UpdatePayload: zoneUpdateFlags,
		Columns: []resource.Column[clouddnsv1.Zone]{
			{Name: "name", Value: func(z *clouddnsv1.Zone) string { return z.Name }},
			{Name: "master", Value: func(z *clouddnsv1.Zone) string { return strconv.FormatBool(z.IsMaster) }},
			{Name: "dnssec-mode", Value: func(z *clouddnsv1.Zone) string { return z.DNSSecMode }},
			{Name: "admin-email", Value: func(z *clouddnsv1.Zone) string { return z.AdminEmail }},
			{Name: "ttl", Value: func(z *clouddnsv1.Zone) string { return strconv.Itoa(z.TTL) }},
		},
	})

	cmd.AddCommand(
		newDNSZoneImportCommand(opts),
		newDNSZoneApplyCommand(opts),
	)

	return cmd
}

// zoneFields registers the flags shared by zone create and update, returning
// the values so each verb can decide which are required.
type zoneFields struct {
	name             *string
	adminEmail       *string
	master           *bool
	dnssecMode       *string
	refresh          *int
	retry            *int
	expire           *int
	ttl              *int
	masterNS         *string
	notifyAllowedIPs *[]string
}

// registerZoneFields declares every zone payload flag except --name, which
// only create takes. Update addresses the zone by its positional name and the
// Engine's update endpoint carries the name only in the body, so a --name there
// would send a different zone_name with no old name anywhere in the request.
// Whether that renames or writes a different zone is not something go-anxcloud
// says, so the CLI does not offer renaming at all.
func registerZoneFields(flags *pflag.FlagSet) zoneFields {
	return zoneFields{
		adminEmail:       flags.String("admin-email", "", "admin email address used in the SOA record"),
		master:           flags.Bool("master", true, "run the zone as master rather than slave"),
		dnssecMode:       flags.String("dnssec-mode", "unvalidated", `DNSSEC mode: "managed" or "unvalidated"`),
		refresh:          flags.Int("refresh", defaultRefresh, "refresh value used in the SOA record"),
		retry:            flags.Int("retry", defaultRetry, "retry value used in the SOA record"),
		expire:           flags.Int("expire", defaultExpire, "expire value used in the SOA record"),
		ttl:              flags.Int("ttl", defaultTTL, "default time to live for NS records"),
		masterNS:         flags.String("master-ns", "", "master name server"),
		notifyAllowedIPs: flags.StringArray("notify-allowed-ip", nil, "IP address allowed to initiate a domain transfer, repeatable"),
	}
}

func zoneCreateFlags(flags *pflag.FlagSet) func(*clouddnsv1.Zone) error {
	fields := registerZoneFields(flags)
	fields.name = flags.String("name", "", "zone name")

	return func(z *clouddnsv1.Zone) error {
		if *fields.name == "" {
			return errmap.Usagef("--name is required")
		}

		if *fields.adminEmail == "" {
			return errmap.Usagef("--admin-email is required")
		}

		z.Name = *fields.name
		z.AdminEmail = *fields.adminEmail
		z.IsMaster = *fields.master
		z.DNSSecMode = *fields.dnssecMode
		z.Refresh = *fields.refresh
		z.Retry = *fields.retry
		z.Expire = *fields.expire
		z.TTL = *fields.ttl
		z.MasterNS = *fields.masterNS
		z.NotifyAllowedIPs = *fields.notifyAllowedIPs

		return nil
	}
}

// zoneUpdateFlags applies only the flags the user actually set, so an update
// names what is changing rather than restating the whole zone. Every field
// keeps the value the preceding read returned unless a flag was passed.
func zoneUpdateFlags(flags *pflag.FlagSet) func(*clouddnsv1.Zone) error {
	fields := registerZoneFields(flags)

	return func(z *clouddnsv1.Zone) error {
		changed := 0

		for name, apply := range map[string]func(){
			"admin-email":       func() { z.AdminEmail = *fields.adminEmail },
			"master":            func() { z.IsMaster = *fields.master },
			"dnssec-mode":       func() { z.DNSSecMode = *fields.dnssecMode },
			"refresh":           func() { z.Refresh = *fields.refresh },
			"retry":             func() { z.Retry = *fields.retry },
			"expire":            func() { z.Expire = *fields.expire },
			"ttl":               func() { z.TTL = *fields.ttl },
			"master-ns":         func() { z.MasterNS = *fields.masterNS },
			"notify-allowed-ip": func() { z.NotifyAllowedIPs = *fields.notifyAllowedIPs },
		} {
			if flags.Changed(name) {
				apply()

				changed++
			}
		}

		if changed == 0 {
			return errmap.Usagef("nothing to update: pass at least one field to change")
		}

		return nil
	}
}
