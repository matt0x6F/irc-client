# Cascade IRC Client - Documentation

This directory contains documentation for the Cascade IRC client project.

## Documentation Index

### [User Stories](./user-stories.md)
Comprehensive list of user stories for features missing from the current implementation. Stories are organized by feature area and include acceptance criteria, priority, and effort estimates.

**Use this for:**
- Understanding what features are planned
- Creating work items and tasks
- Prioritizing development work
- Tracking feature requirements

### [Planning Guide](./planning-guide.md)
Guide for breaking down user stories into actionable tasks and planning development sprints.

**Use this for:**
- Sprint planning
- Task breakdown
- Understanding dependencies
- Tracking progress

### [Technical Documentation](../agents.md)
Architecture and technical implementation details (located in project root).

**Use this for:**
- Understanding system architecture
- Implementation patterns
- Code organization
- Development workflows

### [Events System](./events.md)
Comprehensive documentation of the event-driven architecture, event types, and how to use the EventBus.

**Use this for:**
- Understanding event types and structure
- Subscribing to and emitting events
- Implementing event subscribers
- Event flow and architecture

### [Plugin System](./plugin-system.md)
Complete guide to the plugin architecture, protocol, and how to write plugins.

**Use this for:**
- Understanding plugin architecture
- Writing plugins in any language
- Plugin discovery and lifecycle
- UI metadata system
- JSON-RPC protocol details

## Quick Start

1. **New to the project?** Start with the [Technical Documentation](../agents.md) to understand the architecture.

2. **Planning features?** Review [User Stories](./user-stories.md) to see what's planned.

3. **Breaking down work?** Use the [Planning Guide](./planning-guide.md) to create tasks.

4. **Implementing a feature?** Check the user story acceptance criteria and follow the planning guide.

5. **Working with events?** See the [Events System](./events.md) documentation.

6. **Writing a plugin?** See the [Plugin System](./plugin-system.md) documentation.

## Contributing

When adding new features:

1. Create or update user stories in `user-stories.md`
2. Follow the story format (As a... I want... So that...)
3. Include acceptance criteria
4. Estimate priority and effort
5. Update this README if adding new documentation

## Story Status

Track story status using:
- **Backlog**: Not yet started
- **In Progress**: Currently being worked on
- **Blocked**: Waiting on dependencies
- **Review**: Code complete, awaiting review
- **Done**: Completed and verified

## Questions?

- Check the [Technical Documentation](../agents.md) for implementation details
- Review existing code patterns in the codebase
- Check [User Stories](./user-stories.md) for feature requirements
- See [Events System](./events.md) for event-related questions
- See [Plugin System](./plugin-system.md) for plugin development questions

