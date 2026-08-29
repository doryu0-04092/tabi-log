########################################
# ALB
########################################

resource "aws_lb" "main" {
  name               = "${var.project}-alb"
  load_balancer_type = "application"
  subnets            = aws_subnet.public[*].id
  security_groups    = [aws_security_group.alb.id]

  # **インターネット向けにせざるを得ない。**
  # CloudFront の既定ドメインだけで完結させる構成では、
  # VPC オリジンや内部 ALB は使えない。
  # そのままでは ALB の DNS 名を直接叩けてしまうため、
  # 下のリスナールールで実際に絞る。
  internal = false

  # 学習用途なので消せる状態にしておく。本番では有効にする。
  enable_deletion_protection = false

  tags = { Name = "${var.project}-alb" }
}

resource "aws_lb_target_group" "app" {
  name        = "${var.project}-app"
  port        = 8080
  protocol    = "HTTP"
  target_type = "ip"
  vpc_id      = aws_vpc.main.id

  health_check {
    # **データベースを見ない方を向ける。**
    #
    # 疎通まで見るヘルスチェックをロードバランサに向けると、
    # データベースが一時的に不調になったときに全タスクが同時に
    # unhealthy と判定されて一斉に置き換えられる。
    # **置き換えてもデータベースは回復しない。**
    # タスクを入れ替えるべき理由になるのは「プロセスが生きていないこと」だけである。
    #
    # /api/readyz は投入判定と状況確認に使い、ここには向けない。
    path                = "/api/livez"
    matcher             = "200"
    interval            = 15
    timeout             = 5
    healthy_threshold   = 2
    unhealthy_threshold = 3
  }

  # 新しいタスクへ切り替えるとき、処理中のリクエストを待つ。
  deregistration_delay = 30

  tags = { Name = "${var.project}-app" }
}

# ---------------------------------------------------------------------------
# リスナー
# ---------------------------------------------------------------------------

# **既定は 403 にする。**
#
# セキュリティグループの絞り込みは「CloudFront のエッジから来たこと」しか
# 保証しない。他人が作ったディストリビューションも同じ IP レンジから来る。
# 合言葉のヘッダーを持つものだけを通し、それ以外は本文ごと拒否する。
resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.main.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type = "fixed-response"

    fixed_response {
      content_type = "application/json"
      # 画面が受け取る他のエラーと同じ形にする。
      # 形が違うと、クライアントが解釈に失敗して原因が分からなくなる。
      message_body = jsonencode({
        error = {
          code    = "forbidden"
          message = "このエンドポイントへは直接アクセスできません"
        }
      })
      status_code = "403"
    }
  }
}

resource "aws_lb_listener_rule" "from_cloudfront" {
  listener_arn = aws_lb_listener.http.arn
  priority     = 1

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.app.arn
  }

  condition {
    http_header {
      http_header_name = "X-Origin-Verify"
      values           = [random_password.origin_header.result]
    }
  }
}
