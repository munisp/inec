# INEC Platform — Production Environment
# Lagos (af-south-1) primary, with multi-AZ deployment
# Scale: 48,000 polling units, 100M+ citizens

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
    key            = "production/terraform.tfstate"
    region         = "af-south-1"
    encrypt        = true
    dynamodb_table = "inec-terraform-locks"
  }
}

provider "aws" {
  region = "af-south-1" # Cape Town (closest AWS region to Nigeria)

  default_tags {
    tags = {
      Project     = "inec"
      Environment = "production"
      ManagedBy   = "terraform"
    }
  }
}

locals {
  environment = "production"
  azs         = ["af-south-1a", "af-south-1b", "af-south-1c"]
}

# VPC
module "vpc" {
  source      = "../../modules/vpc"
  environment = local.environment
  azs         = local.azs
}

# EKS Cluster
module "eks" {
  source              = "../../modules/eks"
  environment         = local.environment
  vpc_id              = module.vpc.vpc_id
  private_subnet_ids  = module.vpc.private_subnet_ids
  cluster_version     = "1.30"
  node_instance_types = ["m6i.2xlarge"]
  node_min_size       = 10
  node_max_size       = 80
  node_desired_size   = 20
}

# PostgreSQL (Primary + 2 read replicas)
module "rds" {
  source                = "../../modules/rds"
  environment           = local.environment
  vpc_id                = module.vpc.vpc_id
  private_subnet_ids    = module.vpc.private_subnet_ids
  instance_class        = "db.r6g.4xlarge"
  allocated_storage     = 1000
  max_allocated_storage = 5000
  replica_count         = 2
}

# Redis
module "redis" {
  source             = "../../modules/redis"
  environment        = local.environment
  vpc_id             = module.vpc.vpc_id
  private_subnet_ids = module.vpc.private_subnet_ids
  node_type          = "cache.r6g.2xlarge"
  num_cache_clusters = 3
}

# Outputs
output "eks_cluster_name" { value = module.eks.cluster_name }
output "rds_primary_endpoint" { value = module.rds.primary_endpoint }
output "rds_replica_endpoints" { value = module.rds.replica_endpoints }
output "redis_endpoint" { value = module.redis.primary_endpoint }
