# Role-Based Access Control (RBAC) Overview

This document outlines the access levels for each API endpoint based on user roles.

## Public Endpoints

| Endpoint | Role |
|---|---|
| `GET /health` | `any` |
| `GET /api/v1/version` | `any` |
| `GET /metrics` | `any` |
| `POST /auth/token` | `any` (rate limited) |

## Auth

| Endpoint | Role |
|---|---|
| `GET /auth/me` | `any` |
| `POST /api/v1/auth/reset-token` | `any` authenticated |

## Configuration

| Endpoint | Role |
|---|---|
| `GET /api/v1/config` | `admin` |
| `PUT /api/v1/config` | `admin` |
| `POST /api/v1/config/export` | `admin` |
| `POST /api/v1/config/import` | `admin` |

## Clusters

| Endpoint | Role |
|---|---|
| `POST /api/v1/clusters/test` | `admin` |
| `GET /api/v1/clusters` | `admin`, `operator`, `monitoring`, `compliance` |
| `POST /api/v1/clusters` | `admin` |
| `GET /api/v1/clusters/:name` | `admin`, `operator`, `monitoring`, `compliance` |
| `GET /api/v1/clusters/:name/status` | `admin`, `operator`, `monitoring`, `compliance` |
| `GET /api/v1/clusters/:name/topics` | `admin`, `operator`, `monitoring`, `compliance` |
| `GET /api/v1/clusters/:name/topic-details` | `admin`, `operator`, `monitoring`, `compliance` |
| `PUT /api/v1/clusters/:name` | `admin`, `operator` |
| `DELETE /api/v1/clusters/:name` | `admin` |
| `POST /api/v1/clusters/:name/restore` | `admin` |
| `DELETE /api/v1/clusters/purge` | `admin` |

## Jobs

| Endpoint | Role |
|---|---|
| `GET /api/v1/jobs` | `admin`, `operator`, `monitoring`, `compliance` |
| `POST /api/v1/jobs` | `admin` |
| `GET /api/v1/jobs/:id` | `admin`, `operator`, `monitoring`, `compliance` |
| `PUT /api/v1/jobs/:id` | `admin`, `operator` |
| `DELETE /api/v1/jobs/:id` | `admin` |
| `POST /api/v1/jobs/:id/start` | `admin`, `operator` |
| `POST /api/v1/jobs/:id/stop` | `admin`, `operator` |
| `POST /api/v1/jobs/:id/pause` | `admin`, `operator` |
| `POST /api/v1/jobs/:id/restart` | `admin`, `operator` |
| `POST /api/v1/jobs/:id/force-restart` | `admin`, `operator` |
| `POST /api/v1/jobs/start-all` | `admin`, `operator` |
| `POST /api/v1/jobs/stop-all` | `admin`, `operator` |
| `POST /api/v1/jobs/restart-all` | `admin`, `operator` |
| `GET /api/v1/jobs/:id/mappings` | `admin`, `operator`, `monitoring`, `compliance` |
| `PUT /api/v1/jobs/:id/mappings` | `admin`, `operator` |
| `GET /api/v1/jobs/:id/topic-health` | `admin`, `operator`, `monitoring` |

## Metrics

| Endpoint | Role |
|---|---|
| `GET /api/v1/jobs/:id/metrics/current` | `admin`, `operator`, `monitoring`, `compliance` |
| `GET /api/v1/jobs/:id/metrics/history` | `admin`, `operator`, `monitoring`, `compliance` |
| `GET /api/v1/jobs/:id/lag` | `admin`, `operator`, `monitoring`, `compliance` |

## Topics

| Endpoint | Role |
|---|---|
| `GET /api/v1/topics/source` | `admin`, `operator`, `monitoring`, `compliance` |
| `GET /api/v1/topics/target` | `admin`, `operator`, `monitoring`, `compliance` |

## AI Insights

| Endpoint | Role |
|---|---|
| `GET /api/v1/ai/insights` | `admin`, `operator`, `monitoring` |
| `GET /api/v1/ai/metrics` | `admin`, `operator`, `monitoring` |
| `POST /api/v1/ai/test` | `admin`, `operator` |
| `GET /api/v1/ai/anomalies` | `admin`, `operator`, `monitoring` |
| `GET /api/v1/ai/recommendations` | `admin`, `operator`, `monitoring` |
| `POST /api/v1/ai/explain/:event` | `admin`, `operator`, `monitoring` |
| `POST /api/v1/ai/incidents/:event_id/analyze` | `admin`, `operator` |
| `POST /api/v1/jobs/:id/ai/analyze` | `admin`, `operator` |
| `GET /api/v1/jobs/:id/ai/insights` | `admin`, `operator`, `monitoring` |
| `POST /api/v1/jobs/:id/ai/historical-analysis` | `admin`, `operator` |

## Users

| Endpoint | Role |
|---|---|
| `GET /api/v1/users` | `admin` |
| `POST /api/v1/users` | `admin` |
| `PUT /api/v1/users/:username/role` | `admin` |
| `DELETE /api/v1/users/:username` | `admin` |
| `PUT /api/v1/users/change-password` | `any` |
| `POST /api/v1/users/:username/reset-password` | `admin` |

## Inventory

| Endpoint | Role |
|---|---|
| `GET /api/v1/jobs/:id/inventory/snapshots` | `admin`, `operator` |
| `POST /api/v1/jobs/:id/inventory/snapshots` | `admin`, `operator` |
| `GET /api/v1/inventory/snapshots/:snapshot_id` | `admin`, `operator`, `monitoring`, `compliance` |
| `GET /api/v1/inventory/snapshots/:snapshot_id/cluster` | `admin`, `operator`, `monitoring`, `compliance` |
| `GET /api/v1/inventory/snapshots/:snapshot_id/topics` | `admin`, `operator`, `monitoring`, `compliance` |
| `GET /api/v1/inventory/snapshots/:snapshot_id/consumer-groups` | `admin`, `operator`, `monitoring`, `compliance` |
| `GET /api/v1/inventory/snapshots/:snapshot_id/connections` | `admin`, `operator`, `monitoring`, `compliance` |

## Mirror State

| Endpoint | Role |
|---|---|
| `GET /api/v1/jobs/:id/mirror/state` | `admin`, `operator`, `monitoring` |
| `GET /api/v1/jobs/:id/mirror/progress` | `admin`, `operator`, `monitoring` |
| `GET /api/v1/jobs/:id/mirror/resume-points` | `admin`, `operator`, `monitoring` |
| `POST /api/v1/jobs/:id/mirror/resume-points` | `admin`, `operator` |
| `GET /api/v1/jobs/:id/mirror/gaps` | `admin`, `operator`, `monitoring` |
| `POST /api/v1/jobs/:id/mirror/validate-mirror` | `admin`, `operator` |
| `POST /api/v1/jobs/:id/mirror/checkpoint` | `admin`, `operator` |

## Compliance

| Endpoint | Role |
|---|---|
| `POST /api/v1/compliance/report/:period` | `admin`, `compliance` |
| `GET /api/v1/compliance/reports` | `admin`, `compliance` |
| `GET /api/v1/compliance/report/:id` | `admin`, `compliance` |

## Events

| Endpoint | Role |
|---|---|
| `GET /api/v1/events` | `admin`, `operator`, `monitoring`, `compliance` |
