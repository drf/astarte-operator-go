package deps

import semver "github.com/Masterminds/semver/v3"

// GetDefaultVersionForCFSSL returns the default CFSSL version based on the Astarte version requested
func GetDefaultVersionForCFSSL(astarteVersion *semver.Version) string {
	checkVersion, _ := astarteVersion.SetPrerelease("")
	c, _ := semver.NewConstraint("< 0.11.0")
	if c.Check(&checkVersion) {
		return "1.0.0-astarte.0"
	}

	return "1.4.1-astarte.0"
}

// GetDefaultVersionForCassandra returns the default Cassandra version based on the Astarte version requested
func GetDefaultVersionForCassandra(astarteVersion *semver.Version) string {
	// TODO: We should change this to the official images
	return "v13"
}

// GetDefaultVersionForRabbitMQ returns the default RabbitMQ version based on the Astarte version requested
func GetDefaultVersionForRabbitMQ(astarteVersion *semver.Version) string {
	checkVersion, _ := astarteVersion.SetPrerelease("")
	c, _ := semver.NewConstraint("< 0.11.0")
	if c.Check(&checkVersion) {
		return "3.7.15"
	}

	return "3.7.21"
}
