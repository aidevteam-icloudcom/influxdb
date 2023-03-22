use std::{collections::HashMap, path::PathBuf, sync::Arc};

use arrow::{
    datatypes::{Schema, SchemaRef},
    record_batch::RecordBatch,
};
use arrow_flight::{
    decode::FlightRecordBatchStream,
    sql::{
        Any, CommandGetCatalogs, CommandGetDbSchemas, CommandGetSqlInfo, CommandGetTableTypes,
        CommandGetTables, CommandStatementQuery, ProstMessageExt, SqlInfo,
    },
    FlightClient, FlightDescriptor,
};
use arrow_util::test_util::batches_to_sorted_lines;
use assert_cmd::Command;
use datafusion::common::assert_contains;
use futures::{FutureExt, TryStreamExt};
use influxdb_iox_client::flightsql::FlightSqlClient;
use predicates::prelude::*;
use prost::Message;
use test_helpers_end_to_end::{maybe_skip_integration, MiniCluster, Step, StepTest, StepTestState};

#[tokio::test]
async fn flightsql_adhoc_query() {
    test_helpers::maybe_start_logging();
    let database_url = maybe_skip_integration!();

    let table_name = "the_table";

    // Set up the cluster  ====================================
    let mut cluster = MiniCluster::create_shared2(database_url).await;

    StepTest::new(
        &mut cluster,
        vec![
            Step::WriteLineProtocol(format!(
                "{table_name},tag1=A,tag2=B val=42i 123456\n\
                 {table_name},tag1=A,tag2=C val=43i 123457"
            )),
            Step::Custom(Box::new(move |state: &mut StepTestState| {
                async move {
                    let sql = format!("select * from {table_name}");
                    let mut client = flightsql_client(state.cluster());

                    let stream = client.query(sql).await.unwrap();
                    let batches = collect_stream(stream).await;
                    insta::assert_yaml_snapshot!(
                        batches_to_sorted_lines(&batches),
                        @r###"
                    ---
                    - +------+------+--------------------------------+-----+
                    - "| tag1 | tag2 | time                           | val |"
                    - +------+------+--------------------------------+-----+
                    - "| A    | B    | 1970-01-01T00:00:00.000123456Z | 42  |"
                    - "| A    | C    | 1970-01-01T00:00:00.000123457Z | 43  |"
                    - +------+------+--------------------------------+-----+
                    "###
                    );
                }
                .boxed()
            })),
        ],
    )
    .run()
    .await
}

#[tokio::test]
async fn flightsql_adhoc_query_error() {
    test_helpers::maybe_start_logging();
    let database_url = maybe_skip_integration!();

    // Set up the cluster  ====================================
    let mut cluster = MiniCluster::create_shared2(database_url).await;

    StepTest::new(
        &mut cluster,
        vec![
            Step::WriteLineProtocol(
                "foo,tag1=A,tag2=B val=42i 123456\n\
                 foo,tag1=A,tag2=C val=43i 123457"
                    .to_string(),
            ),
            Step::Custom(Box::new(move |state: &mut StepTestState| {
                async move {
                    let sql = String::from("select * from incorrect_table");

                    let mut client = flightsql_client(state.cluster());

                    let err = client.query(sql).await.unwrap_err();

                    // namespaces are created on write
                    assert_contains!(
                        err.to_string(),
                        "table 'public.iox.incorrect_table' not found"
                    );
                }
                .boxed()
            })),
        ],
    )
    .run()
    .await
}

#[tokio::test]
async fn flightsql_prepared_query() {
    test_helpers::maybe_start_logging();
    let database_url = maybe_skip_integration!();

    let table_name = "the_table";

    // Set up the cluster  ====================================
    let mut cluster = MiniCluster::create_shared2(database_url).await;

    StepTest::new(
        &mut cluster,
        vec![
            Step::WriteLineProtocol(format!(
                "{table_name},tag1=A,tag2=B val=42i 123456\n\
                 {table_name},tag1=A,tag2=C val=43i 123457"
            )),
            Step::Custom(Box::new(move |state: &mut StepTestState| {
                async move {
                    let sql = format!("select * from {table_name}");
                    let mut client = flightsql_client(state.cluster());

                    let handle = client.prepare(sql).await.unwrap();
                    let stream = client.execute(handle).await.unwrap();

                    let batches = collect_stream(stream).await;
                    insta::assert_yaml_snapshot!(
                        batches_to_sorted_lines(&batches),
                        @r###"
                    ---
                    - +------+------+--------------------------------+-----+
                    - "| tag1 | tag2 | time                           | val |"
                    - +------+------+--------------------------------+-----+
                    - "| A    | B    | 1970-01-01T00:00:00.000123456Z | 42  |"
                    - "| A    | C    | 1970-01-01T00:00:00.000123457Z | 43  |"
                    - +------+------+--------------------------------+-----+
                    "###
                    );
                }
                .boxed()
            })),
        ],
    )
    .run()
    .await
}

#[tokio::test]
async fn flightsql_get_sql_infos() {
    test_helpers::maybe_start_logging();
    let database_url = maybe_skip_integration!();

    let table_name = "the_table";

    // Set up the cluster  ====================================
    let mut cluster = MiniCluster::create_shared2(database_url).await;

    StepTest::new(
        &mut cluster,
        vec![
            Step::WriteLineProtocol(format!(
                "{table_name},tag1=A,tag2=B val=42i 123456\n\
                 {table_name},tag1=A,tag2=C val=43i 123457"
            )),
            Step::Custom(Box::new(move |state: &mut StepTestState| {
                async move {
                    let mut client = flightsql_client(state.cluster());

                    // test with no filtering
                    let batches = collect_stream(client.get_sql_info(vec![]).await.unwrap()).await;
                    let total_rows: usize = batches.iter().map(|b| b.num_rows()).sum();
                    // 85 `SqlInfo` entries are returned by IOx's GetSqlInfo implementation
                    // if we change what is returned then this number should be updated too
                    assert_eq!(total_rows, 85);

                    // only retrieve requested metadata
                    let infos = vec![
                        SqlInfo::FlightSqlServerName as u32,
                        SqlInfo::FlightSqlServerArrowVersion as u32,
                        SqlInfo::SqlBatchUpdatesSupported as u32,
                        999999, //  model some unknown info requested
                    ];

                    let batches = collect_stream(client.get_sql_info(infos).await.unwrap()).await;

                    insta::assert_yaml_snapshot!(
                        batches_to_sorted_lines(&batches),
                        @r###"
                    ---
                    - +-----------+-----------------------------+
                    - "| info_name | value                       |"
                    - +-----------+-----------------------------+
                    - "| 0         | {string_value=InfluxDB IOx} |"
                    - "| 2         | {string_value=1.3}          |"
                    - "| 572       | {bool_value=false}          |"
                    - +-----------+-----------------------------+
                    "###
                    );

                    // Test zero case (nothing matches)
                    let infos = vec![
                        999999, //  model some unknown info requested
                    ];

                    let batches = collect_stream(client.get_sql_info(infos).await.unwrap()).await;

                    insta::assert_yaml_snapshot!(
                        batches_to_sorted_lines(&batches),
                        @r###"
                    ---
                    - ++
                    - ++
                    "###
                    );
                }
                .boxed()
            })),
        ],
    )
    .run()
    .await
}

#[tokio::test]
async fn flightsql_get_catalogs() {
    test_helpers::maybe_start_logging();
    let database_url = maybe_skip_integration!();

    let table_name = "the_table";

    // Set up the cluster  ====================================
    let mut cluster = MiniCluster::create_shared2(database_url).await;

    StepTest::new(
        &mut cluster,
        vec![
            Step::WriteLineProtocol(format!(
                "{table_name},tag1=A,tag2=B val=42i 123456\n\
                 {table_name},tag1=A,tag2=C val=43i 123457"
            )),
            Step::Custom(Box::new(move |state: &mut StepTestState| {
                async move {
                    let mut client = flightsql_client(state.cluster());

                    let stream = client.get_catalogs().await.unwrap();
                    let batches = collect_stream(stream).await;

                    insta::assert_yaml_snapshot!(
                        batches_to_sorted_lines(&batches),
                        @r###"
                    ---
                    - +--------------+
                    - "| catalog_name |"
                    - +--------------+
                    - "| public       |"
                    - +--------------+
                    "###
                    );
                }
                .boxed()
            })),
        ],
    )
    .run()
    .await
}

#[tokio::test]
async fn flightsql_get_tables() {
    test_helpers::maybe_start_logging();
    let database_url = maybe_skip_integration!();

    let table_name = "the_table";

    // Set up the cluster  ====================================
    let mut cluster = MiniCluster::create_shared2(database_url).await;

    StepTest::new(
        &mut cluster,
        vec![
            Step::WriteLineProtocol(format!(
                "{table_name},tag1=A,tag2=B val=42i 123456\n\
                 {table_name},tag1=A,tag2=C val=43i 123457"
            )),
            Step::Custom(Box::new(move |state: &mut StepTestState| {
                async move {
                    struct TestCase {
                        catalog: Option<&'static str>,
                        db_schema_filter_pattern: Option<&'static str>,
                        table_name_filter_pattern: Option<&'static str>,
                        table_types: Vec<String>,
                        include_schema: bool,
                    }
                    let cases = [
                        TestCase {
                            catalog: None,
                            db_schema_filter_pattern: None,
                            table_name_filter_pattern: None,
                            table_types: vec![],
                            include_schema: false,
                        },
                        TestCase {
                            catalog: None,
                            db_schema_filter_pattern: None,
                            table_name_filter_pattern: None,
                            table_types: vec!["BASE TABLE".to_string()],
                            include_schema: false,
                        },
                        TestCase {
                            catalog: None,
                            db_schema_filter_pattern: None,
                            table_name_filter_pattern: None,
                            // BASE <> BASE TABLE
                            table_types: vec!["BASE".to_string()],
                            include_schema: false,
                        },
                        TestCase {
                            catalog: None,
                            db_schema_filter_pattern: None,
                            table_name_filter_pattern: None,
                            table_types: vec!["RANDOM".to_string()],
                            include_schema: false,
                        },
                        TestCase {
                            catalog: Some("public"),
                            db_schema_filter_pattern: Some("information_schema"),
                            table_name_filter_pattern: Some("tables"),
                            table_types: vec!["VIEW".to_string()],
                            include_schema: false,
                        },
                    ];

                    let mut client = flightsql_client(state.cluster());

                    let mut output = vec![];
                    for case in cases {
                        let TestCase {
                            catalog,
                            db_schema_filter_pattern,
                            table_name_filter_pattern,
                            table_types,
                            include_schema,
                        } = case;

                        output.push(format!("catalog:{catalog:?}"));
                        output.push(format!(
                            "db_schema_filter_pattern:{db_schema_filter_pattern:?}"
                        ));
                        output.push(format!(
                            "table_name_filter_pattern:{table_name_filter_pattern:?}"
                        ));
                        output.push(format!("table_types:{table_types:?}"));
                        output.push(format!("include_schema:{include_schema:?}"));
                        output.push("*********************".into());

                        let stream = client
                            .get_tables(
                                catalog,
                                db_schema_filter_pattern,
                                table_name_filter_pattern,
                                table_types,
                            )
                            .await
                            .unwrap();
                        let batches = collect_stream(stream).await;
                        output.extend(batches_to_sorted_lines(&batches))
                    }

                    insta::assert_yaml_snapshot!(
                        output,
                        @r###"
                    ---
                    - "catalog:None"
                    - "db_schema_filter_pattern:None"
                    - "table_name_filter_pattern:None"
                    - "table_types:[]"
                    - "include_schema:false"
                    - "*********************"
                    - +--------------+--------------------+-------------+------------+
                    - "| catalog_name | db_schema_name     | table_name  | table_type |"
                    - +--------------+--------------------+-------------+------------+
                    - "| public       | information_schema | columns     | VIEW       |"
                    - "| public       | information_schema | df_settings | VIEW       |"
                    - "| public       | information_schema | tables      | VIEW       |"
                    - "| public       | information_schema | views       | VIEW       |"
                    - "| public       | iox                | the_table   | BASE TABLE |"
                    - "| public       | system             | queries     | BASE TABLE |"
                    - +--------------+--------------------+-------------+------------+
                    - "catalog:None"
                    - "db_schema_filter_pattern:None"
                    - "table_name_filter_pattern:None"
                    - "table_types:[\"BASE TABLE\"]"
                    - "include_schema:false"
                    - "*********************"
                    - +--------------+----------------+------------+------------+
                    - "| catalog_name | db_schema_name | table_name | table_type |"
                    - +--------------+----------------+------------+------------+
                    - "| public       | iox            | the_table  | BASE TABLE |"
                    - "| public       | system         | queries    | BASE TABLE |"
                    - +--------------+----------------+------------+------------+
                    - "catalog:None"
                    - "db_schema_filter_pattern:None"
                    - "table_name_filter_pattern:None"
                    - "table_types:[\"BASE\"]"
                    - "include_schema:false"
                    - "*********************"
                    - ++
                    - ++
                    - "catalog:None"
                    - "db_schema_filter_pattern:None"
                    - "table_name_filter_pattern:None"
                    - "table_types:[\"RANDOM\"]"
                    - "include_schema:false"
                    - "*********************"
                    - ++
                    - ++
                    - "catalog:Some(\"public\")"
                    - "db_schema_filter_pattern:Some(\"information_schema\")"
                    - "table_name_filter_pattern:Some(\"tables\")"
                    - "table_types:[\"VIEW\"]"
                    - "include_schema:false"
                    - "*********************"
                    - +--------------+--------------------+------------+------------+
                    - "| catalog_name | db_schema_name     | table_name | table_type |"
                    - +--------------+--------------------+------------+------------+
                    - "| public       | information_schema | tables     | VIEW       |"
                    - +--------------+--------------------+------------+------------+
                    "###
                    );
                }
                .boxed()
            })),
        ],
    )
    .run()
    .await
}

#[tokio::test]
async fn flightsql_get_table_types() {
    test_helpers::maybe_start_logging();
    let database_url = maybe_skip_integration!();

    let table_name = "the_table";

    // Set up the cluster  ====================================
    let mut cluster = MiniCluster::create_shared2(database_url).await;

    StepTest::new(
        &mut cluster,
        vec![
            Step::WriteLineProtocol(format!(
                "{table_name},tag1=A,tag2=B val=42i 123456\n\
                 {table_name},tag1=A,tag2=C val=43i 123457"
            )),
            Step::Custom(Box::new(move |state: &mut StepTestState| {
                async move {
                    let mut client = flightsql_client(state.cluster());

                    let stream = client.get_table_types().await.unwrap();
                    let batches = collect_stream(stream).await;

                    insta::assert_yaml_snapshot!(
                        batches_to_sorted_lines(&batches),
                        @r###"
                    ---
                    - +------------+
                    - "| table_type |"
                    - +------------+
                    - "| BASE TABLE |"
                    - "| VIEW       |"
                    - +------------+
                    "###
                    );
                }
                .boxed()
            })),
        ],
    )
    .run()
    .await
}

#[tokio::test]
async fn flightsql_get_db_schemas() {
    test_helpers::maybe_start_logging();
    let database_url = maybe_skip_integration!();

    let table_name = "the_table";

    // Set up the cluster  ====================================
    let mut cluster = MiniCluster::create_shared2(database_url).await;

    StepTest::new(
        &mut cluster,
        vec![
            Step::WriteLineProtocol(format!(
                "{table_name},tag1=A,tag2=B val=42i 123456\n\
                 {table_name},tag1=A,tag2=C val=43i 123457"
            )),
            Step::Custom(Box::new(move |state: &mut StepTestState| {
                async move {
                    struct TestCase {
                        catalog: Option<&'static str>,
                        db_schema_filter_pattern: Option<&'static str>,
                    }
                    let cases = [
                        TestCase {
                            catalog: None,
                            db_schema_filter_pattern: None,
                        },
                        TestCase {
                            // pub <> public
                            catalog: Some("pub"),
                            db_schema_filter_pattern: None,
                        },
                        TestCase {
                            // pub% should match all
                            catalog: Some("pub%"),
                            db_schema_filter_pattern: None,
                        },
                        TestCase {
                            catalog: None,
                            db_schema_filter_pattern: Some("%for%"),
                        },
                        TestCase {
                            catalog: Some("public"),
                            db_schema_filter_pattern: Some("iox"),
                        },
                    ];

                    let mut client = flightsql_client(state.cluster());

                    let mut output = vec![];
                    for case in cases {
                        let TestCase {
                            catalog,
                            db_schema_filter_pattern,
                        } = case;
                        output.push(format!("catalog:{catalog:?}"));
                        output.push(format!(
                            "db_schema_filter_pattern:{db_schema_filter_pattern:?}"
                        ));
                        output.push("*********************".into());

                        let stream = client
                            .get_db_schemas(catalog, db_schema_filter_pattern)
                            .await
                            .unwrap();
                        let batches = collect_stream(stream).await;
                        output.extend(batches_to_sorted_lines(&batches))
                    }
                    insta::assert_yaml_snapshot!(
                        output,
                        @r###"
                    ---
                    - "catalog:None"
                    - "db_schema_filter_pattern:None"
                    - "*********************"
                    - +--------------+--------------------+
                    - "| catalog_name | db_schema_name     |"
                    - +--------------+--------------------+
                    - "| public       | information_schema |"
                    - "| public       | iox                |"
                    - "| public       | system             |"
                    - +--------------+--------------------+
                    - "catalog:Some(\"pub\")"
                    - "db_schema_filter_pattern:None"
                    - "*********************"
                    - ++
                    - ++
                    - "catalog:Some(\"pub%\")"
                    - "db_schema_filter_pattern:None"
                    - "*********************"
                    - +--------------+--------------------+
                    - "| catalog_name | db_schema_name     |"
                    - +--------------+--------------------+
                    - "| public       | information_schema |"
                    - "| public       | iox                |"
                    - "| public       | system             |"
                    - +--------------+--------------------+
                    - "catalog:None"
                    - "db_schema_filter_pattern:Some(\"%for%\")"
                    - "*********************"
                    - +--------------+--------------------+
                    - "| catalog_name | db_schema_name     |"
                    - +--------------+--------------------+
                    - "| public       | information_schema |"
                    - +--------------+--------------------+
                    - "catalog:Some(\"public\")"
                    - "db_schema_filter_pattern:Some(\"iox\")"
                    - "*********************"
                    - +--------------+----------------+
                    - "| catalog_name | db_schema_name |"
                    - +--------------+----------------+
                    - "| public       | iox            |"
                    - +--------------+----------------+
                    "###
                    );
                }
                .boxed()
            })),
        ],
    )
    .run()
    .await
}

#[tokio::test]
/// Runs  the `jdbc_client` program against IOx to verify JDBC via FlightSQL is working
///
/// Example command:
///
/// ```shell
/// TEST_INFLUXDB_JDBC=true TEST_INFLUXDB_IOX_CATALOG_DSN=postgresql://postgres@localhost:5432/postgres cargo test --test end_to_end  jdbc
/// ```
async fn flightsql_jdbc() {
    test_helpers::maybe_start_logging();
    let database_url = maybe_skip_integration!();

    if std::env::var("TEST_INFLUXDB_JDBC").ok().is_none() {
        println!("Skipping JDBC test because TEST_INFLUXDB_JDBC is not set");
        return;
    }

    let table_name = "the_table";

    // Set up the cluster  ====================================
    let mut cluster = MiniCluster::create_shared2(database_url).await;

    StepTest::new(
        &mut cluster,
        vec![
            Step::WriteLineProtocol(format!(
                "{table_name},tag1=A,tag2=B val=42i 123456\n\
                 {table_name},tag1=A,tag2=C val=43i 123457"
            )),
            Step::Custom(Box::new(move |state: &mut StepTestState| {
                // satisfy the borrow checker
                async move {
                    let namespace = state.cluster().namespace();

                    // querier_addr looks like: http://127.0.0.1:8092
                    let querier_addr = state.cluster().querier().querier_grpc_base().to_string();
                    println!("Querier {querier_addr}, namespace {namespace}");

                    // JDBC URL looks like this:
                    // jdbc:arrow-flight-sql://localhost:8082?useEncryption=false&iox-namespace-name=26f7e5a4b7be365b_917b97a92e883afc
                    let jdbc_addr = querier_addr.replace("http://", "jdbc:arrow-flight-sql://");
                    let jdbc_url =
                        format!("{jdbc_addr}?useEncryption=false&iox-namespace-name={namespace}");
                    println!("jdbc_url {jdbc_url}");

                    // find the jdbc_client to run
                    let path = PathBuf::from(std::env::var("PWD").expect("can not get PWD"))
                        .join("influxdb_iox/tests/jdbc_client/jdbc_client");
                    println!("Path to jdbc client: {path:?}");

                    // Validate basic query: jdbc_client <url> query 'sql'
                    Command::from_std(std::process::Command::new(&path))
                        .arg(&jdbc_url)
                        .arg("query")
                        .arg(format!("select * from {table_name} order by time"))
                        .arg(&querier_addr)
                        .assert()
                        .success()
                        .stdout(predicate::str::contains("Running SQL Query"))
                        .stdout(predicate::str::contains(
                            "A,  B,  1970-01-01 00:00:00.000123456,  42",
                        ))
                        .stdout(predicate::str::contains(
                            "A,  C,  1970-01-01 00:00:00.000123457,  43",
                        ));

                    // Validate prepared query: jdbc_client <url> prepared_query 'sql'
                    Command::from_std(std::process::Command::new(&path))
                        .arg(&jdbc_url)
                        .arg("prepared_query")
                        .arg(format!("select tag1, tag2 from {table_name} order by time"))
                        .arg(&querier_addr)
                        .assert()
                        .success()
                        .stdout(predicate::str::contains("Running Prepared SQL Query"))
                        .stdout(predicate::str::contains("A,  B"));

                    // CommandGetCatalogs output
                    let expected_catalogs = "**************\n\
                                             Catalogs:\n\
                                             **************\n\
                                             TABLE_CAT\n\
                                             ------------\n\
                                             public";

                    // CommandGetSchemas output
                    let expected_schemas = "**************\n\
                                            Schemas:\n\
                                            **************\n\
                                            TABLE_SCHEM,  TABLE_CATALOG\n\
                                            ------------\n\
                                            information_schema,  public\n\
                                            iox,  public\n\
                                            system,  public";

                    // CommandGetTables output
                    let expected_tables_no_filter = "**************\n\
                                           Tables:\n\
                                           **************\n\
                                           TABLE_CAT,  TABLE_SCHEM,  TABLE_NAME,  TABLE_TYPE,  REMARKS,  TYPE_CAT,  TYPE_SCHEM,  TYPE_NAME,  SELF_REFERENCING_COL_NAME,  REF_GENERATION\n\
                                           ------------\n\
                                           public,  information_schema,  columns,  VIEW,  null,  null,  null,  null,  null,  null\n\
                                           public,  information_schema,  df_settings,  VIEW,  null,  null,  null,  null,  null,  null\n\
                                           public,  information_schema,  tables,  VIEW,  null,  null,  null,  null,  null,  null\n\
                                           public,  information_schema,  views,  VIEW,  null,  null,  null,  null,  null,  null\n\
                                           public,  iox,  the_table,  BASE TABLE,  null,  null,  null,  null,  null,  null\n\
                                           public,  system,  queries,  BASE TABLE,  null,  null,  null,  null,  null,  null";

                    // CommandGetTables output
                    let expected_tables_with_filters = "**************\n\
                                            Tables (system table filter):\n\
                                            **************\n\
                                            TABLE_CAT,  TABLE_SCHEM,  TABLE_NAME,  TABLE_TYPE,  REMARKS,  TYPE_CAT,  TYPE_SCHEM,  TYPE_NAME,  SELF_REFERENCING_COL_NAME,  REF_GENERATION\n\
                                            ------------\n\
                                            public,  system,  queries,  BASE TABLE,  null,  null,  null,  null,  null,  null";

                    // CommandGetTableTypes output
                    let expected_table_types = "**************\n\
                                                Table Types:\n\
                                                **************\n\
                                                TABLE_TYPE\n\
                                                ------------\n\
                                                BASE TABLE\n\
                                                VIEW";

                    // Validate metadata: jdbc_client <url> metadata
                    let mut assert = Command::from_std(std::process::Command::new(&path))
                        .arg(&jdbc_url)
                        .arg("metadata")
                        .assert()
                        .success()
                        .stdout(predicate::str::contains(expected_catalogs))
                        .stdout(predicate::str::contains(expected_schemas))
                        .stdout(predicate::str::contains(expected_tables_no_filter))
                        .stdout(predicate::str::contains(expected_tables_with_filters))
                        .stdout(predicate::str::contains(expected_table_types));

                    let expected_metadata = EXPECTED_METADATA
                        .trim()
                        .replace("REPLACE_ME_WITH_JBDC_URL", &jdbc_url);

                    for expected in expected_metadata.lines() {
                        assert = assert.stdout(predicate::str::contains(expected));
                    }
                }
                .boxed()
            })),
        ],
    )
    .run()
    .await
}

/// test ensures that the schema returned as part of GetFlightInfo matches that of the
/// actual response.
#[tokio::test]
async fn flightsql_schema_matches() {
    test_helpers::maybe_start_logging();
    let database_url = maybe_skip_integration!();

    let table_name = "the_table";

    // Set up the cluster  ====================================
    let mut cluster = MiniCluster::create_shared2(database_url).await;

    StepTest::new(
        &mut cluster,
        vec![
            Step::WriteLineProtocol(format!(
                "{table_name},tag1=A,tag2=B val=42i 123456\n\
                 {table_name},tag1=A,tag2=C val=43i 123457"
            )),
            Step::Custom(Box::new(move |state: &mut StepTestState| {
                async move {
                    let mut client = flightsql_client(state.cluster()).into_inner();

                    // Verify schema for each type of command
                    let cases = vec![
                        CommandStatementQuery {
                            query: format!("select * from {table_name}"),
                        }
                        .as_any(),
                        CommandGetSqlInfo { info: vec![] }.as_any(),
                        CommandGetCatalogs {}.as_any(),
                        CommandGetDbSchemas {
                            catalog: None,
                            db_schema_filter_pattern: None,
                        }
                        .as_any(),
                        CommandGetTables {
                            catalog: None,
                            db_schema_filter_pattern: None,
                            table_name_filter_pattern: None,
                            table_types: vec![],
                            include_schema: false,
                        }
                        .as_any(),
                        CommandGetTableTypes {}.as_any(),
                    ];

                    for cmd in cases {
                        assert_schema(&mut client, cmd).await;
                    }
                }
                .boxed()
            })),
        ],
    )
    .run()
    .await
}

///  Verifies that the schema returned by `GetFlightInfo` and `DoGet`
///  match for `cmd`.
async fn assert_schema(client: &mut FlightClient, cmd: Any) {
    println!("Checking schema for message type {}", cmd.type_url);

    let descriptor = FlightDescriptor::new_cmd(cmd.encode_to_vec());
    let flight_info = client.get_flight_info(descriptor).await.unwrap();

    assert_eq!(flight_info.endpoint.len(), 1);
    let ticket = flight_info.endpoint[0]
        .ticket
        .as_ref()
        .expect("Need ticket")
        .clone();

    // Schema reported by `GetFlightInfo`
    let flight_info_schema = flight_info.try_decode_schema().unwrap();

    // Get results and ensure they match the schema reported by GetFlightInfo
    let mut result_stream = client.do_get(ticket).await.unwrap();
    let mut saw_data = false;
    while let Some(batch) = result_stream.try_next().await.unwrap() {
        saw_data = true;
        // strip metadata (GetFlightInfo doesn't include metadata for
        // some reason) before comparison
        // https://github.com/influxdata/influxdb_iox/issues/7282
        let batch_schema = strip_metadata(&batch.schema());
        assert_eq!(
            batch_schema.as_ref(),
            &flight_info_schema,
            "batch_schema:\n{batch_schema:#?}\n\nflight_info_schema:\n{flight_info_schema:#?}"
        );
        // The stream itself also may report a schema
        if let Some(stream_schema) = result_stream.schema() {
            // strip metadata (GetFlightInfo doesn't include metadata for
            // some reason) before comparison
            // https://github.com/influxdata/influxdb_iox/issues/7282
            let stream_schema = strip_metadata(stream_schema);
            assert_eq!(stream_schema.as_ref(), &flight_info_schema);
        }
    }
    // verify we have seen at least one RecordBatch
    // (all FlightSQL endpoints return at least one)
    assert!(saw_data);
}

fn strip_metadata(schema: &Schema) -> SchemaRef {
    let stripped_fields: Vec<_> = schema
        .fields()
        .iter()
        .map(|f| f.clone().with_metadata(HashMap::new()))
        .collect();

    Arc::new(Schema::new(stripped_fields))
}

/// Return a [`FlightSqlClient`] configured for use
fn flightsql_client(cluster: &MiniCluster) -> FlightSqlClient {
    let connection = cluster.querier().querier_grpc_connection();
    let (channel, _headers) = connection.into_grpc_connection().into_parts();

    let mut client = FlightSqlClient::new(channel);

    // Add namespace to client headers until it is fully supported by FlightSQL
    let namespace = cluster.namespace();
    client.add_header("iox-namespace-name", namespace).unwrap();

    client
}

async fn collect_stream(stream: FlightRecordBatchStream) -> Vec<RecordBatch> {
    stream.try_collect().await.expect("collecting batches")
}

const EXPECTED_METADATA: &str = r#"
allProceduresAreCallable: true
allTablesAreSelectable: true
autoCommitFailureClosesAllResultSets: false
dataDefinitionCausesTransactionCommit: false
dataDefinitionIgnoredInTransactions: true
doesMaxRowSizeIncludeBlobs: true
generatedKeyAlwaysReturned: false
getCatalogSeparator: .
getCatalogTerm: null
getDatabaseMajorVersion: 10
getDatabaseMinorVersion: 0
getDatabaseProductName: InfluxDB IOx
getDatabaseProductVersion: 2
getDefaultTransactionIsolation: 0
getDriverMajorVersion: 10
getDriverMinorVersion: 0
getDriverName: Arrow Flight SQL JDBC Driver
getDriverVersion: 10.0.0
getExtraNameCharacters:
getIdentifierQuoteString: "
getJDBCMajorVersion: 4
getJDBCMinorVersion: 1
getMaxBinaryLiteralLength: 2147483647
getMaxCatalogNameLength: 2147483647
getMaxCharLiteralLength: 2147483647
getMaxColumnNameLength: 2147483647
getMaxColumnsInGroupBy: 2147483647
getMaxColumnsInIndex: 2147483647
getMaxColumnsInOrderBy: 2147483647
getMaxColumnsInSelect: 2147483647
getMaxColumnsInTable: 2147483647
getMaxConnections: 2147483647
getMaxCursorNameLength: 2147483647
getMaxIndexLength: 2147483647
getMaxLogicalLobSize: 0
getMaxProcedureNameLength: 2147483647
getMaxRowSize: 2147483647
getMaxSchemaNameLength: 2147483647
getMaxStatementLength: 2147483647
getMaxStatements: 2147483647
getMaxTableNameLength: 2147483647
getMaxTablesInSelect: 2147483647
getMaxUserNameLength: 2147483647
getNumericFunctions: abs, acos, asin, atan, atan2, ceil, cos, exp, floor, ln, log, log10, log2, pow, power, round, signum, sin, sqrt, tan, trunc
getProcedureTerm: procedure
getResultSetHoldability: 1
getSchemaTerm: schema
getSearchStringEscape: \
getSQLKeywords: absolute, action, add, all, allocate, alter, and, any, are, as, asc, assertion, at, authorization, avg, begin, between, bit, bit_length, both, by, cascade, cascaded, case, cast, catalog, char, char_length, character, character_length, check, close, coalesce, collate, collation, column, commit, connect, connection, constraint, constraints, continue, convert, corresponding, count, create, cross, current, current_date, current_time, current_timestamp, current_user, cursor, date, day, deallocate, dec, decimal, declare, default, deferrable, deferred, delete, desc, describe, descriptor, diagnostics, disconnect, distinct, domain, double, drop, else, end, end-exec, escape, except, exception, exec, execute, exists, external, extract, false, fetch, first, float, for, foreign, found, from, full, get, global, go, goto, grant, group, having, hour, identity, immediate, in, indicator, initially, inner, input, insensitive, insert, int, integer, intersect, interval, into, is, isolation, join, key, language, last, leading, left, level, like, local, lower, match, max, min, minute, module, month, names, national, natural, nchar, next, no, not, null, nullif, numeric, octet_length, of, on, only, open, option, or, order, outer, output, overlaps, pad, partial, position, precision, prepare, preserve, primary, prior, privileges, procedure, public, read, real, references, relative, restrict, revoke, right, rollback, rows, schema, scroll, second, section, select, session, session_user, set, size, smallint, some, space, sql, sqlcode, sqlerror, sqlstate, substring, sum, system_user, table, temporary, then, time, timestamp, timezone_hour, timezone_minute, to, trailing, transaction, translate, translation, trim, true, union, unique, unknown, update, upper, usage, user, using, value, values, varchar, varying, view, when, whenever, where, with, work, write, year, zone
getSQLStateType: 2
getStringFunctions: arrow_typeof, ascii, bit_length, btrim, char_length, character_length, chr, concat, concat_ws, digest, from_unixtime, initcap, left, length, lower, lpad, ltrim, md5, octet_length, random, regexp_match, regexp_replace, repeat, replace, reverse, right, rpad, rtrim, sha224, sha256, sha384, sha512, split_part, starts_with, strpos, substr, to_hex, translate, trim, upper, uuid
getSystemFunctions: array, arrow_typeof, struct
getTimeDateFunctions: current_date, current_time, date_bin, date_part, date_trunc, datepart, datetrunc, from_unixtime, now, to_timestamp, to_timestamp_micros, to_timestamp_millis, to_timestamp_seconds
getURL: REPLACE_ME_WITH_JBDC_URL
getUserName: test
isCatalogAtStart: false
isReadOnly: true
locatorsUpdateCopy: false
nullPlusNonNullIsNull: true
nullsAreSortedAtEnd: true
nullsAreSortedAtStart: false
nullsAreSortedHigh: false
nullsAreSortedLow: false
storesLowerCaseIdentifiers: false
storesLowerCaseQuotedIdentifiers: false
storesMixedCaseIdentifiers: false
storesMixedCaseQuotedIdentifiers: false
storesUpperCaseIdentifiers: true
storesUpperCaseQuotedIdentifiers: false
supportsAlterTableWithAddColumn: false
supportsAlterTableWithDropColumn: false
supportsANSI92EntryLevelSQL: true
supportsANSI92FullSQL: true
supportsANSI92IntermediateSQL: true
supportsBatchUpdates: false
supportsCatalogsInDataManipulation: true
supportsCatalogsInIndexDefinitions: false
supportsCatalogsInPrivilegeDefinitions: false
supportsCatalogsInProcedureCalls: true
supportsCatalogsInTableDefinitions: true
supportsColumnAliasing: true
supportsCoreSQLGrammar: false
supportsCorrelatedSubqueries: true
supportsDataDefinitionAndDataManipulationTransactions: false
supportsDataManipulationTransactionsOnly: true
supportsDifferentTableCorrelationNames: false
supportsExpressionsInOrderBy: true
supportsExtendedSQLGrammar: false
supportsFullOuterJoins: false
supportsGetGeneratedKeys: false
supportsGroupBy: true
supportsGroupByBeyondSelect: true
supportsGroupByUnrelated: true
supportsIntegrityEnhancementFacility: false
supportsLikeEscapeClause: true
supportsLimitedOuterJoins: true
supportsMinimumSQLGrammar: true
supportsMixedCaseIdentifiers: false
supportsMixedCaseQuotedIdentifiers: true
supportsMultipleOpenResults: false
supportsMultipleResultSets: false
supportsMultipleTransactions: false
supportsNamedParameters: false
supportsNonNullableColumns: true
supportsOpenCursorsAcrossCommit: false
supportsOpenCursorsAcrossRollback: false
supportsOpenStatementsAcrossCommit: false
supportsOpenStatementsAcrossRollback: false
supportsOrderByUnrelated: true
supportsOuterJoins: true
supportsPositionedDelete: false
supportsPositionedUpdate: false
supportsRefCursors: false
supportsSavepoints: false
supportsSchemasInDataManipulation: true
supportsSchemasInIndexDefinitions: false
supportsSchemasInPrivilegeDefinitions: false
supportsSchemasInProcedureCalls: false
supportsSchemasInTableDefinitions: true
supportsSelectForUpdate: false
supportsStatementPooling: false
supportsStoredFunctionsUsingCallSyntax: false
supportsStoredProcedures: false
supportsSubqueriesInComparisons: true
supportsSubqueriesInExists: true
supportsSubqueriesInIns: true
supportsSubqueriesInQuantifieds: true
supportsTableCorrelationNames: false
supportsTransactionIsolationLevel: false
supportsTransactions: false
supportsUnion: true
supportsUnionAll: true
usesLocalFilePerTable: false
usesLocalFiles: false
"#;
