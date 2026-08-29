# **秘密は出力しない。** 出力すると `terraform output` で平文になり、
# CI のログにも残る。必要なら Parameter Store から取る。

output "app_url" {
  description = "アプリケーションの入口。ここだけを利用者に伝える"
  value       = "https://${aws_cloudfront_distribution.main.domain_name}"
}

output "cloudfront_distribution_id" {
  description = "デプロイ後に index.html を無効化するために使う"
  value       = aws_cloudfront_distribution.main.id
}

output "static_bucket" {
  description = "フロントエンドの成果物を置く先（aws s3 sync の宛先）"
  value       = aws_s3_bucket.static.id
}

output "images_bucket" {
  description = "投稿画像の保存先"
  value       = aws_s3_bucket.images.id
}

output "ecr_repository_url" {
  description = "バックエンドのイメージを push する先"
  value       = aws_ecr_repository.backend.repository_url
}

output "ecs_cluster" {
  description = "強制デプロイ（aws ecs update-service）で使う"
  value       = aws_ecs_cluster.main.name
}

output "ecs_service" {
  description = "同上"
  value       = aws_ecs_service.backend.name
}

output "db_endpoint" {
  description = <<-EOT
    データベースの接続先。

    **プライベートサブネットにあるため、手元からは直接届かない。**
    マイグレーションを流すには、ECS のタスクから実行するか、
    一時的に踏み台を用意する必要がある（README.md）。
  EOT
  value       = aws_db_instance.main.address
}

output "alb_dns_name" {
  description = <<-EOT
    ALB の DNS 名。**確認用であり、ここを利用者に伝えてはいけない。**
    直接叩いても、合言葉のヘッダーが無いため 403 が返る
    （それが期待どおりの動作である）。
  EOT
  value       = aws_lb.main.dns_name
}

output "log_groups" {
  description = "ログの見に行き先"
  value = {
    backend     = aws_cloudwatch_log_group.backend.name
    imageworker = aws_cloudwatch_log_group.imageworker.name
  }
}
