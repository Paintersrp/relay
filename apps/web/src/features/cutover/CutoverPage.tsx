import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { AppPageFrame } from "@/components/relay/AppPageFrame";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { getCutoverState, getCutoverHistory, rollbackCutover, type CutoverState } from "./api";

export function CutoverPage() {
  const queryClient = useQueryClient();

  const stateQuery = useQuery({ queryKey: ["cutover", "state"], queryFn: getCutoverState, refetchInterval: 5000 });
  const historyQuery = useQuery({ queryKey: ["cutover", "history"], queryFn: getCutoverHistory });

  const rollbackMutation = useMutation({
    mutationFn: (activationId: string) => rollbackCutover(activationId),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ["cutover"] }); },
  });

  const state: CutoverState | undefined = stateQuery.data;
  const isActive = state?.active ?? false;
  const currentState = state?.state;

  return (
    <AppPageFrame title="Cutover" description="Manage the ticket-oriented admission cutover lifecycle.">
      <div className="space-y-6">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-3">
              Current Mode
              {isActive && currentState ? (
                <Badge variant={currentState.status === "active" ? "default" : "secondary"}>
                  {currentState.status}
                </Badge>
              ) : (
                <Badge variant="outline">inactive</Badge>
              )}
            </CardTitle>
          </CardHeader>
          <CardContent>
            {isActive && currentState ? (
              <div className="space-y-2 text-sm">
                <div className="flex gap-2">
                  <span className="font-medium">Activation:</span>
                  <code className="text-xs">{currentState.activationId}</code>
                </div>
                <div className="flex gap-2">
                  <span className="font-medium">Boundary:</span>
                  <Badge variant={currentState.boundaryStatus === "crossed" ? "default" : "outline"}>
                    {currentState.boundaryStatus}
                  </Badge>
                </div>
                <div className="flex gap-2">
                  <span className="font-medium">Rollback:</span>
                  <Badge variant={currentState.rollbackStatus === "available" ? "default" : "destructive"}>
                    {currentState.rollbackStatus}
                  </Badge>
                </div>
                <div className="flex gap-2">
                  <span className="font-medium">Roll Forward:</span>
                  <Badge variant={currentState.rollForwardStatus === "required" ? "default" : "outline"}>
                    {currentState.rollForwardStatus}
                  </Badge>
                </div>
                {currentState.activatedAt && (
                  <div className="flex gap-2">
                    <span className="font-medium">Activated:</span>
                    <span>{currentState.activatedAt}</span>
                  </div>
                )}
                <div className="flex gap-2 pt-4">
                  {currentState.status === "active" && currentState.boundaryStatus === "open" &&
                   currentState.rollbackStatus === "available" && (
                    <Button variant="destructive" size="sm"
                      disabled={rollbackMutation.isPending}
                      onClick={() => rollbackMutation.mutate(currentState.activationId)}>
                      Rollback Activation
                    </Button>
                  )}
                </div>
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">No active cutover. Legacy admission is open.</p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Activation History</CardTitle>
          </CardHeader>
          <CardContent>
            {historyQuery.data?.items && historyQuery.data.items.length > 0 ? (
              <div className="space-y-2">
                {historyQuery.data.items.map((item) => (
                  <div key={item.activationId} className="flex items-center gap-3 text-sm border-b pb-2">
                    <code className="text-xs">{item.activationId}</code>
                    <Badge variant={item.status === "active" ? "default" : item.status === "rolled_back" ? "destructive" : "secondary"}>
                      {item.status}
                    </Badge>
                    <span className="text-muted-foreground">{item.boundaryStatus}</span>
                    {item.activatedAt && <span className="text-muted-foreground text-xs">{item.activatedAt}</span>}
                  </div>
                ))}
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">No activation history.</p>
            )}
          </CardContent>
        </Card>
      </div>
    </AppPageFrame>
  );
}
