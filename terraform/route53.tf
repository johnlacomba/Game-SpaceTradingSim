data "aws_route53_zone" "primary" {
  count        = var.domain_name != "" ? 1 : 0
  name         = var.domain_name
  private_zone = false
}

resource "aws_route53_record" "apex_a" {
  count   = var.domain_name != "" && var.apex_a_record_ip != "" ? 1 : 0
  zone_id = data.aws_route53_zone.primary[0].zone_id
  name    = var.domain_name
  type    = "A"
  ttl     = 300
  records = [var.apex_a_record_ip]

  depends_on = [data.aws_route53_zone.primary]
}

output "route53_zone_id" {
  description = "Hosted zone ID for the primary domain"
  value       = try(data.aws_route53_zone.primary[0].zone_id, null)
}

output "route53_name_servers" {
  description = "Name server records for the hosted zone (update these at your registrar)"
  value       = try(data.aws_route53_zone.primary[0].name_servers, [])
}