# INEC Platform — Staging Environment
# Smaller replica of production for pre-election testing

terraform {
  required_version = ">= 1.6"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

  backend "s3" {
    bucket         = "inec-terraform-state"
    key            = "staging/terraform.tfstate"
    region         = "af-south-1"
    encrypt        = true
    dynamodb_table = "inec-terraform-locks"
  }
}

provider "aws" {
  region = "af-south-1"

  default_tags {
    tags = {
      Project     = "inec"
      Environment = "staging"
      ManagedBy   = "terraform"
    }
  }
}

locals {
  environment = "staging"
  azs         = ["af-south-1a", "af-south-1b"]
}

module "vpc" {
  source      = "../../modules/vpc"
  environment = local.environment
  azs         = local.azs
}

module "eks" {
  source              = "../../modules/eks"
  environment         = local.environment
  vpc_id              = module.vpc.vpc_id
  private_subnet_ids  = module.vpc.private_subnet_ids
  cluster_version     = "1.30"
  node_instance_types = ["m6i.xlarge"]
  node_min_size       = 3
  node_max_size       = 15
  node_desired_size   = 5
}

module "rds" {
  source                = "../../modules/rds"
  environment           = local.environment
  vpc_id                = module.vpc.vpc_id
  private_subnet_ids    = module.vpc.private_subnet_ids
  instance_class        = "db.r6g.xlarge"
  allocated_storage     = 200
  max_allocated_storage = 500
  replica_count         = 1
}

module "redis" {
  source             = "../../modules/redis"
  environment        = local.environment
  vpc_id             = module.vpc.vpc_id
  private_subnet_ids = module.vpc.private_subnet_ids
  node_type          = "cache.r6g.large"
  num_cache_clusters = 2
}

output "eks_cluster_name" { value = module.eks.cluster_name }
output "rds_primary_endpoint" { value = module.rds.primary_endpoint }
output "redis_endpoint" { value = module.redis.primary_endpoint }
