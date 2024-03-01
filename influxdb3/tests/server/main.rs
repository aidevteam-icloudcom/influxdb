use std::{
    net::{SocketAddr, SocketAddrV4, TcpListener},
    process::{Child, Command, Stdio},
    time::Duration,
};

use assert_cmd::cargo::CommandCargoExt;
use influxdb3_client::Precision;
use influxdb_iox_client::flightsql::FlightSqlClient;
use reqwest::Response;

mod auth;
mod flight;
mod query;

/// A running instance of the `influxdb3 serve` process
pub struct TestServer {
    bind_addr: SocketAddr,
    server_process: Child,
    http_client: reqwest::Client,
}

impl TestServer {
    /// Spawn a new [`TestServer`]
    ///
    /// This will run the `influxdb3 serve` command, and bind its HTTP
    /// address to a random port on localhost.
    pub async fn spawn() -> Self {
        let bind_addr = get_local_bind_addr();
        let mut command = Command::cargo_bin("influxdb3").expect("create the influxdb3 command");
        let command = command
            .arg("serve")
            .args(["--http-bind", &bind_addr.to_string()])
            .args(["--object-store", "memory"])
            // TODO - other configuration can be passed through
            .stdout(Stdio::null())
            .stderr(Stdio::null());

        let server_process = command.spawn().expect("spawn the influxdb3 server process");

        let server = Self {
            bind_addr,
            server_process,
            http_client: reqwest::Client::new(),
        };

        server.wait_until_ready().await;
        server
    }

    /// Get the URL of the running service for use with an HTTP client
    pub fn client_addr(&self) -> String {
        format!("http://{addr}", addr = self.bind_addr)
    }

    /// Get a [`FlightSqlClient`] for making requests to the running service over gRPC
    pub async fn flight_client(&self, database: &str) -> FlightSqlClient {
        let channel = tonic::transport::Channel::from_shared(self.client_addr())
            .expect("create tonic channel")
            .connect()
            .await
            .expect("connect to gRPC client");
        let mut client = FlightSqlClient::new(channel);
        client.add_header("database", database).unwrap();
        client.add_header("iox-debug", "true").unwrap();
        client
    }

    fn kill(&mut self) {
        self.server_process.kill().expect("kill the server process");
    }

    async fn wait_until_ready(&self) {
        while self
            .http_client
            .get(format!("{base}/health", base = self.client_addr()))
            .send()
            .await
            .is_err()
        {
            tokio::time::sleep(Duration::from_millis(10)).await;
        }
    }
}

impl Drop for TestServer {
    fn drop(&mut self) {
        self.kill();
    }
}

impl TestServer {
    /// Write some line protocol to the server
    pub async fn write_lp_to_db(&self, database: &str, lp: &'static str, precision: Precision) {
        let client = influxdb3_client::Client::new(self.client_addr()).unwrap();
        client
            .api_v3_write_lp(database)
            .body(lp)
            .precision(precision)
            .send()
            .await
            .unwrap();
    }

    pub async fn api_v3_query_influxql(&self, params: &[(&str, &str)]) -> Response {
        self.http_client
            .get(format!(
                "{base}/api/v3/query_influxql",
                base = self.client_addr()
            ))
            .query(params)
            .send()
            .await
            .expect("send /api/v3/query_influxql request to server")
    }
}

/// Get an available bind address on localhost
///
/// This binds a [`TcpListener`] to 127.0.0.1:0, which will randomly
/// select an available port, and produces the resulting local address.
/// The [`TcpListener`] is dropped at the end of the function, thus
/// freeing the port for use by the caller.
fn get_local_bind_addr() -> SocketAddr {
    let ip = std::net::Ipv4Addr::new(127, 0, 0, 1);
    let port = 0;
    let addr = SocketAddrV4::new(ip, port);
    TcpListener::bind(addr)
        .expect("bind to a socket address")
        .local_addr()
        .expect("get local address")
}
