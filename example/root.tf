variable "url" {
  default = "localhost"
}

variable "username" {}

variable "password" {}

variable "database_primary" {}

variable "database_test" {
  default = "testdb"
}

// Set up connection to Redshift
provider redshift {
  url      = "${var.url}"
  user     = "${var.username}"
  password = "${var.password}"
  database = "${var.database_primary}"
  sslmode  = "disable"
}

// Create an initial user
resource "redshift_user" "testuser" {
  username         = "testusernew"
  password         = "Testpass123"
  connection_limit = "4"
  createdb         = true
}

// Create another user
resource "redshift_user" "testuser2" {
  username         = "testuser8"
  password         = "Testpass123"
  connection_limit = "1"
  createdb         = true
}

// Create a group with the two above-created users
resource "redshift_group" "testgroup" {
  group_name = "testgroup"
  users      = ["${redshift_user.testuser.id}", "${redshift_user.testuser2.id}"]
}

// Create a `testschemax` schema
resource "redshift_schema" "testschema" {
  schema_name       = "testschemax"
  cascade_on_delete = true
}

// Create a `testscratch` schema with a quota limit
resource "redshift_schema" "test_quota_limited_schema" {
  schema_name = "testschemay"
  quota       = 4096
}

// Add priviliges for the above-created schema
resource "redshift_group_schema_privilege" "testgroup_testchema_privileges" {
  schema_id = "${redshift_schema.testschema.id}"
  group_id  = "${redshift_group.testgroup.id}"
  select    = true
  insert    = true
  update    = true
}
