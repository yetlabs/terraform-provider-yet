# Every minute, on every stage: a POST to the application's own path — on a
# runtime from a CronJob beside it, on Lambda from EventBridge Scheduler.
resource "yet_cloud_application_schedule" "reconcile" {
  application_id = yet_cloud_application.api.id
  name           = "reconcile"
  cron           = "* * * * *"
  path           = "/api/cron/reconcile"
}

# At three on weekdays, in São Paulo's time, on production alone.
resource "yet_cloud_application_schedule" "nightly" {
  application_id = yet_cloud_application.api.id
  name           = "nightly"
  cron           = "0 3 * * 1-5"
  time_zone      = "America/Sao_Paulo"
  method         = "GET"
  path           = "/jobs/nightly?full=1"
  stages         = [yet_cloud_environment.production.id]
}
