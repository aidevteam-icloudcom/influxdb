#!/bin/bash -eux

service influxdb start
result=$(curl -s -o /dev/null -H "Content-Type: application/json" -XPOST -d '{\"username\": \"default\", \"password\": \"thisisnotused\", \"retentionPeriodSeconds\": 0, \"org\": \"test-org\", \"bucket\": \"unused_bucket\", \"token\": \"thisisatesttoken\"}' http://localhost:8086/api/v2/setup -w %{http_code})
service influxdb stop
service influxdb start
service influxdb stop
