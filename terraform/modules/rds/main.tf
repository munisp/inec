# INEC Platform — RDS PostgreSQL Module
# Multi-AZ PostgreSQL 16 with PostGIS, automated backups, encryption at rest

variable "project" { default = "inec" }
variable "environment" { type = string }
variable "vpc_id" { type = string }
variable "private_subnet_ids" { type = list(string) }
variable "instance_class" { default = "db.r6g.2xlarge" }
variable "allocated_storage" { default = 500 }
variable "max_allocated_storage" { default = 2000 }
variable "db_name" { default = "inec" }
variable "db_username" { default = "inec_admin" }
variable "replica_count" { default = 2 }

# DB Subnet Group
resource "aws_db_subnet_group" "main" {
  name       = "${var.project}-${var.environment}-db"
  subnet_ids = var.private_subnet_ids
  tags       = { Name = "${var.project}-${var.environment}-db-subnet" }
}

# Security Group for RDS
resource "aws_security_group" "rds" {
  name_prefix = "${var.project}-${var.environment}-rds-"
  vpc_id      = var.vpc_id

  ingress {
    from_port   = 5432
    to_port     = 5432
    protocol    = "tcp"
    cidr_blocks = ["10.0.0.0/16"]
    description = "PostgreSQL from VPC"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# KMS Key for RDS encryption
resource "aws_kms_key" "rds" {
  description             = "RDS encryption key for ${var.project}-${var.environment}"
  deletion_window_in_days = 30
  enable_key_rotation     = true
}

# RDS Parameter Group with PostGIS
resource "aws_db_parameter_group" "main" {
  name_prefix = "${var.project}-${var.environment}-pg16-"
  family      = "postgres16"

  parameter {
    name  = "shared_preload_libraries"
    value = "pg_stat_statements,postgis-3"
  }

  parameter {
    name  = "log_statement"
    value = "mod"
  }

  parameter {
    name  = "log_min_duration_statement"
    value = "100"
  }

  parameter {
    name         = "max_connections"
    value        = "500"
    apply_method = "pending-reboot"
  }

  parameter {
    name  = "ssl"
    value = "1"
  }

  lifecycle {
    create_before_destroy = true
  }
}

# Primary RDS Instance
resource "aws_db_instance" "primary" {
  identifier     = "${var.project}-${var.environment}-primary"
  engine         = "postgres"
  engine_version = "16.4"
  instance_class = var.instance_class

  allocated_storage     = var.allocated_storage
  max_allocated_storage = var.max_allocated_storage
  storage_type          = "gp3"
  storage_encrypted     = true
  kms_key_id            = aws_kms_key.rds.arn

  db_name  = var.db_name
  username = var.db_username
  manage_master_user_password = true

  db_subnet_group_name   = aws_db_subnet_group.main.name
  vpc_security_group_ids = [aws_security_group.rds.id]
  parameter_group_name   = aws_db_parameter_group.main.name

  multi_az = true

  backup_retention_period   = 35
  backup_window             = "02:00-03:00"
  maintenance_window        = "sun:04:00-sun:05:00"
  copy_tags_to_snapshot     = true
  deletion_protection       = true
  skip_final_snapshot       = false
  final_snapshot_identifier = "${var.project}-${var.environment}-final"

  performance_insights_enabled    = true
  performance_insights_kms_key_id = aws_kms_key.rds.arn

  enabled_cloudwatch_logs_exports = ["postgresql", "upgrade"]

  tags = {
    Name        = "${var.project}-${var.environment}-primary"
    Environment = var.environment
  }
}

# Read Replicas
resource "aws_db_instance" "replica" {
  count               = var.replica_count
  identifier          = "${var.project}-${var.environment}-replica-${count.index}"
  replicate_source_db = aws_db_instance.primary.identifier
  instance_class      = var.instance_class
  storage_encrypted   = true
  kms_key_id          = aws_kms_key.rds.arn

  vpc_security_group_ids = [aws_security_group.rds.id]
  parameter_group_name   = aws_db_parameter_group.main.name

  performance_insights_enabled    = true
  performance_insights_kms_key_id = aws_kms_key.rds.arn

  tags = {
    Name        = "${var.project}-${var.environment}-replica-${count.index}"
    Environment = var.environment
    Role        = "read-replica"
  }
}

output "primary_endpoint" { value = aws_db_instance.primary.endpoint }
output "replica_endpoints" { value = aws_db_instance.replica[*].endpoint }
output "db_name" { value = aws_db_instance.primary.db_name }
