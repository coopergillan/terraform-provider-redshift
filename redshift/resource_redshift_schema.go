package redshift

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

/*
TODO
Add database property. This will require a new connection since you can't have databse agnostic connections in redshift/postgres
*/

func redshiftSchema() *schema.Resource {
	return &schema.Resource{
		Create: resourceRedshiftSchemaCreate,
		Read:   resourceRedshiftSchemaRead,
		Update: resourceRedshiftSchemaUpdate,
		Delete: resourceRedshiftSchemaDelete,
		Exists: resourceRedshiftSchemaExists,
		Importer: &schema.ResourceImporter{
			State: resourceRedshiftSchemaImport,
		},

		Schema: map[string]*schema.Schema{
			"schema_name": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "This is not immutable, but it probably should be!",
			},
			"owner": {
				Type:        schema.TypeInt,
				Optional:    true,
				Computed:    true,
				Description: "Defaults to user specified in provider",
			},
			"cascade_on_delete": {
				Type:        schema.TypeBool,
				Optional:    true,
				Description: "Keyword that indicates to automatically drop all objects in the schema, such as tables and functions. By default it doesn't for your safety",
				Default:     false,
			},
			"quota": {
				Type:        schema.TypeInt,
				Optional:    true,
				Description: "In megabytes, the maximum amount of disk space that the specified schema can use",
				Default:     0,
			},
			"fail_if_exists": {
				Type:        schema.TypeInt,
				Optional:    true,
				Description: "Avoid creating objects if already exists in the DB",
				Default:     true,
			},
		},
	}
}

func resourceRedshiftSchemaExists(d *schema.ResourceData, meta interface{}) (b bool, e error) {
	// Exists - This is called to verify a resource still exists. It is called prior to Read,
	// and lowers the burden of Read to be able to assume the resource exists.
	client := meta.(*Client).db

	var name string

	var existenceQuery = "SELECT nspname FROM pg_namespace WHERE oid = $1"

	log.Print("Does schema exist query: " + existenceQuery + ", " + d.Id())

	err := client.QueryRow(existenceQuery, d.Id()).Scan(&name)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, err
	}
	return true, nil
}

func resourceRedshiftSchemaCreate(d *schema.ResourceData, meta interface{}) error {

	redshiftClient := meta.(*Client).db

	if d.Get("fail_if_exists").(bool) == false {

		exists, err := existsSchemaName(redshiftClient, d.Get("schema_name").(string))

		if err != nil {
			return fmt.Errorf("Could not create redshift schema %s", err)
		}

		if exists == true {
			log.Printf("Schema Name %s, already exists in the database, skipping schema creation", d.Get("schema_name").(string))
			return nil
		}
	}

	var createStatement string = "CREATE SCHEMA " + d.Get("schema_name").(string)

	//If an owner is specified, set authorization with mapped username
	if v, ok := d.GetOk("owner"); ok {
		var usernames = GetUsersnamesForUsesysid(redshiftClient, []interface{}{v.(int)})
		createStatement += " AUTHORIZATION " + usernames[0]
	}

	//If no quota is specified it defaults to unlimited
	if v, ok := d.GetOk("quota"); ok && v.(int) != 0 {
		createStatement += " QUOTA " + strconv.Itoa(v.(int)) + " MB"
	} else {
		createStatement += " QUOTA UNLIMITED"
	}

	log.Print("Create Schema statement: " + createStatement)

	if _, err := redshiftClient.Exec(createStatement); err != nil {
		log.Print(err)
		return err
	}

	//The changes do not propagate instantly
	time.Sleep(5 * time.Second)

	var oid string

	err := redshiftClient.QueryRow("SELECT oid FROM pg_namespace WHERE nspname = $1", d.Get("schema_name").(string)).Scan(&oid)

	if err != nil {
		log.Print(err)
		return err
	}

	log.Print("Created schema with oid: " + oid)

	d.SetId(oid)

	readErr := readRedshiftSchema(d, redshiftClient)

	return readErr
}

func resourceRedshiftSchemaRead(d *schema.ResourceData, meta interface{}) error {

	redshiftClient := meta.(*Client).db

	err := readRedshiftSchema(d, redshiftClient)

	return err
}

func readRedshiftSchema(d *schema.ResourceData, db *sql.DB) error {
	var (
		schemaName string
		owner      int
		quota      int
	)

	err := db.QueryRow(`
			SELECT trim(nspname) AS nspname, nspowner, coalesce(quota, 0) AS quota
			FROM pg_namespace LEFT JOIN svv_schema_quota_state
				ON svv_schema_quota_state.schema_id = pg_namespace.oid
			WHERE pg_namespace.oid = $1`, d.Id()).Scan(&schemaName, &owner, &quota)

	if err != nil {
		log.Print(err)
		return err
	}

	d.Set("schema_name", schemaName)
	d.Set("owner", owner)
	d.Set("quota", quota)

	return nil
}

func resourceRedshiftSchemaUpdate(d *schema.ResourceData, meta interface{}) error {

	redshiftClient := meta.(*Client).db
	tx, txErr := redshiftClient.Begin()
	if txErr != nil {
		panic(txErr)
	}

	if d.HasChange("schema_name") {

		oldName, newName := d.GetChange("schema_name")
		alterSchemaNameQuery := "ALTER SCHEMA " + oldName.(string) + " RENAME TO " + newName.(string)

		if _, err := tx.Exec(alterSchemaNameQuery); err != nil {
			return err
		}
	}

	if d.HasChange("owner") {

		var username = GetUsersnamesForUsesysid(redshiftClient, []interface{}{d.Get("owner").(int)})

		if _, err := tx.Exec("ALTER SCHEMA " + d.Get("schema_name").(string) + " OWNER TO " + username[0]); err != nil {
			return err
		}
	}

	if d.HasChange("quota") {
		quota := "UNLIMITED"

		if v, ok := d.GetOk("quota"); ok && v.(int) != 0 {
			quota = strconv.Itoa(v.(int)) + " MB"
		}

		if _, err := tx.Exec("ALTER SCHEMA " + d.Get("schema_name").(string) + " QUOTA " + quota); err != nil {
			return err
		}
	}

	err := readRedshiftSchema(d, redshiftClient)

	if err != nil {
		tx.Rollback()
		return err
	}

	commitErr := tx.Commit()
	if commitErr != nil {
		log.Print("Error committing transaction: ", commitErr)
		return commitErr
	}

	return nil
}

func resourceRedshiftSchemaDelete(d *schema.ResourceData, meta interface{}) error {

	client := meta.(*Client).db

	dropSchemaQuery := "DROP SCHEMA " + d.Get("schema_name").(string)

	if v, ok := d.GetOk("cascade_on_delete"); ok && v.(bool) {
		dropSchemaQuery += " CASCADE "
	}

	_, err := client.Exec(dropSchemaQuery)

	if err != nil {
		log.Print(err)
		return err
	}

	return nil
}

func resourceRedshiftSchemaImport(d *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
	if err := resourceRedshiftSchemaRead(d, meta); err != nil {
		return nil, err
	}
	return []*schema.ResourceData{d}, nil
}

func GetSchemaInfoForSchemaId(q Queryer, schemaId int) (string, int, error) {

	var name string
	var owner int

	err := q.QueryRow("SELECT nspname, nspowner FROM pg_namespace WHERE oid = $1", schemaId).Scan(&name, &owner)
	switch {
	case err == sql.ErrNoRows:
		//Is this a good idea?
		return "", -1, err
	case err != nil:
		return "", -1, err
	}
	return name, owner, nil
}

func existsSchemaName(q Queryer, schemaName string) (bool, error) {

	var name string

	err := q.QueryRow("SELECT schemaname FROM pg_tables WHERE schemaname = $1", schemaName).Scan(&name)
	switch {
	case err == sql.ErrNoRows:
		//Is this a good idea?
		return false, err
	case err != nil:
		return false, err
	}
	return true, nil

}
