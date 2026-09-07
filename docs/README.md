# External Arbiter Operator Documentation

This directory contains comprehensive documentation for the external-arbiter-operator project.

## Documentation Files

### [ARCHITECTURE.md](./ARCHITECTURE.md)
Detailed architecture diagrams and technical design documentation covering:
- High-level system architecture across source and remote clusters
- Component interaction flows and reconciliation loops
- RemoteCluster and RemoteArbiter controller workflows
- Ceph configuration extraction and transformation
- Permission model and RBAC requirements
- State machines and design patterns

**Use this when:** You need to understand how the operator works internally, the reconciliation logic, or the cross-cluster communication patterns.

### [DEPLOYMENT-FLOW.md](./DEPLOYMENT-FLOW.md)
Step-by-step deployment guide with visual timeline showing:
- Complete deployment phases from prerequisites to production
- Operator installation and setup procedures
- RemoteCluster and RemoteArbiter resource creation
- Arbiter pod startup and Ceph quorum formation
- Quick reference commands for deployment
- Troubleshooting decision trees for common issues

**Use this when:** You're deploying the operator for the first time, troubleshooting deployment issues, or need to understand the deployment timeline.

## Quick Start

For initial deployment, start with:
1. Read the main [README.md](../README.md) for project overview
2. Follow [DEPLOYMENT-FLOW.md](./DEPLOYMENT-FLOW.md) Phase 1-2 for prerequisites and operator installation
3. Use the "Quick Deployment Reference" section for command-by-command guidance
4. Refer to [ARCHITECTURE.md](./ARCHITECTURE.md) if you need deeper understanding of the reconciliation process

## Additional Resources

- [Main README](../README.md) - Project overview and quick start
- [Contributing Guide](../CONTRIBUTING.md) - How to contribute to the project
- [Helm Chart Values](../contrib/charts/external-arbiter-operator/values.yaml) - Configuration options
- [Example Resources](../contrib/k8s/examples/) - Sample CR definitions

## Diagram Format

All diagrams in this documentation use ASCII art for maximum compatibility. They can be viewed in:
- Any text editor
- Terminal/console
- GitHub/GitLab web interface
- Documentation viewers (Markdown renderers)

No special tools or plugins are required to view the diagrams.
