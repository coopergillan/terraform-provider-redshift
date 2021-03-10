# Terraform Redshift Provider

![Build Status](https://github.com/coopergillan/terraform-provider-redshift/actions/workflows/go.yml/badge.svg)

Manage Redshift users, groups, privileges, databases and schemas. It runs the
SQL queries necessary to manage these (CREATE USER, DELETE DATABASE etc) in
transactions, and also reads the state from the tables that store this state,
eg `pg_user_info`, `pg_group` etc. The underlying tables are more or less
equivalent to the postgres tables, but some tables are not accessible in
Redshift.

Currently supports users, groups, schemas and databases. You can set privileges
for groups on schemas. Per user schema privileges will be added at a later
date.

Note that schemas are the lowest level of granularity here. Tables should be
created by some other tool, for instance flyway.

# Support

This module supports terraform version >0.12 for Redshift versions >1.0.14677.

# Get it:

1. Navigate to the [releases] and download the desired plugin binary, likely the latest
1. [Add to terraform plugins directory][installing_plugin] installed
1. Run `terraform init` to register the plugin in your project

## Legacy download links (0.0.2)

See [original fork](https://github.com/frankfarrell/terraform-provider-redshift) for older download links

## Examples:

### Provider configuration

```terraform
provider redshift {
  url      = "localhost"
  user     = "testroot"
  password = "Rootpass123"
  database = "dev"
}
```

Creating an admin user who is in a group and who owns a new database, with a password that expires

### Create a user

```terraform
resource "redshift_user" "testuser"{
  username         = "testusernew" # User names are not immutable.
  # Terraform can't read passwords, so if the user changes their password it will not be picked up. One caveat is that when the user name is changed, the password is reset to this value
  password         = "Testpass123" # You can pass an md5 encryted password here by prefixing the hash with md5
  valid_until      = "2018-10-30" # See below for an example with 'password_disabled'
  connection_limit = "4"
  createdb         = true
  syslog_access    = "UNRESTRICTED"
  superuser        = true
}
```

### Add the user to a new group

```terraform
resource "redshift_group" "testgroup" {
  group_name = "testgroup" # Group names are not immutable
  users      = ["${redshift_user.testuser.id}"] # A list of user ids as output by terraform (from the pg_user_info table), not a list of usernames (they are not immnutable)
}
```

### Create a schema

```terraform
resource "redshift_schema" "testschema" {
  schema_name       = "testschema"  # Schema names are not immutable
  owner             = "${redshift_user.testuser.id}"  # This defaults to the current user (eg as specified in the provider config) if empty
  cascade_on_delete = true
  quota             = 2048  # in MB
}
```

Note that quotas can either be 0 (unlimited) or [some number greater than the
Redshift minimums][redshift-schema-parameters].

### Give that group select, insert and references privileges on that schema

```terraform
resource "redshift_group_schema_privilege" "testgroup_testchema_privileges" {
  schema_id  = "${redshift_schema.testschema.id}" # Id rather than group name
  group_id   = "${redshift_group.testgroup.id}" # Id rather than group name
  select     = true
  insert     = true
  update     = false
  references = true
  delete     = false # False values are optional
}
```

You can only create resources in the db configured in the provider block. Since
you cannot configure providers with the output of resources, if you want to
create a db and configure resources you will need to configure it through a
`terraform_remote_state` data provider. Even if you specifiy the name directly
rather than as a variable, since providers are configured before resources you
will need to have them in separate projects.

### First file:

```terraform
resource "redshift_database" "testdb" {
  database_name    = "testdb"  # This isn't immutable
  owner            = "${redshift_user.testuser.id}"
  connection_limit = "4"
}

output "testdb_name" {
  value = "${redshift_database.testdb.database_name}"
}
```

### Second file:

```terraform
data "terraform_remote_state" "redshift" {
  backend  = "s3"
  config   = {
    bucket = "somebucket"
    key    = "somekey"
    region = "us-east-1"
  }
}

provider redshift {
  url      = "localhost"
  user     = "testroot"
  password = "Rootpass123"
  database = "${data.terraform_remote_state.redshift.testdb_name}"
}
```

### Creating a user who can only connect using IAM Credentials as described [here](https://docs.aws.amazon.com/redshift/latest/mgmt/generating-user-credentials.html)

```terraform
resource "redshift_user" "testuser" {
  username          = "testusernew"
  password_disabled = true # No need to specify a password if this is true
  connection_limit  = "1"
}
```

## Things to note
### Limitations
For authoritative limitations, please see [the Redshift documentation](https://docs.aws.amazon.com/redshift/index.html).
1) You cannot delete the database you are currently connected to.
2) You cannot set table-specific privileges since, for now,  this provider is
table-agnostic
3) On importing a user, it is impossible to read the password (or even the md
hash of the password, since Redshift restricts access to pg_shadow)

### I usually connect through an ssh tunnel, what do I do?
The easiest thing is probably to update your hosts file so that the url resolves to localhost

## Contributing:

### Prequisites to development
1. [Go installed](https://golang.org/dl/)
2. [Terraform installed locally](https://www.terraform.io/downloads.html)

### Testing

Run the tests

```bash
make test
```

Find unchecked errors

```bash
make errcheck
```

### Building
Run `make dist` to generate binaries for the supported os/architectures. This
process relies on GNUMake and bash, but you can always fallback to generating
your own binaries with `go build -o your-binary-here`.

Once generated, you can add the binary to your terraform plugins directory to
get it working. (e.g.
terraform.d/linux/amd64/terraform-provider-redshift_vblah) Note that the prefix
of the binary must match, and follow guidelines for [Terraform
directories][installing_plugin]

After installing the plugin you can debug crudely by setting the TF_LOG env
variable to DEBUG. Eg

```
$ TF_LOG=DEBUG terraform apply
```

### Releasing

Update the `VERSION` file to the new release number, then run `make release`.
You will be prompted for the prior version to auto-generate a changelog entry.
Review the diffs in `CHANGELOG.md` before committing.

Create and push a tag using the version in `VERSION`:

```
git tag -m $(cat VERSION) $(cat VERSION)
git push origin $(cat VERSION)
```

This will start a Github Actions workflow defined in `.github/workflows/release.yml`
that will generate the binaries and create the release. Releases will be automatically
published [to the Terraform registry](https://registry.terraform.io/providers/coopergillan/redshift/latest).

## TODO

1. Database property for Schema
2. Schema privileges on a per user basis
3. Add privileges for languages and functions

[installing_plugin]: https://www.terraform.io/docs/extend/how-terraform-works.html#implied-local-mirror-directories
[releases]: https://github.com/coopergillan/terraform-provider-redshift/releases
[redshift-schema-parameters]: https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_SCHEMA.html#r_CREATE_SCHEMA-parameters
