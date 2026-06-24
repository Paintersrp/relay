import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/projects/$projectId/refactor-backlog')({
  component: RouteComponent,
})

function RouteComponent() {
  return <div>Hello "/projects/$projectId/refactor-backlog"!</div>
}
