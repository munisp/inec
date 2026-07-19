/**
 * JoinCampaign — handles magic-link invite acceptance
 * Route: /join?token=<hex>
 */
import { useEffect, useState } from "react";
import { useLocation } from "wouter";
import { trpc } from "@/lib/trpc";
import { useAuth } from "@/_core/hooks/useAuth";
import { startLogin } from "@/const";
import { Button } from "@/components/ui/button";
import { Loader2, CheckCircle, XCircle, Users } from "lucide-react";
import { toast } from "sonner";

export default function JoinCampaign() {
  const [, navigate] = useLocation();
  const { isAuthenticated, loading: authLoading } = useAuth();
  const token = new URLSearchParams(window.location.search).get("token") ?? "";

  const { data: invite, isLoading: inviteLoading, error: inviteError } =
    trpc.team.acceptInvite.useQuery({ token }, { enabled: !!token });

  const confirmMut = trpc.team.confirmAccept.useMutation({
    onSuccess: () => {
      toast.success("You've joined the campaign team!");
      navigate("/");
    },
    onError: (e) => toast.error(e.message),
  });

  const handleAccept = () => {
    if (!token) return;
    confirmMut.mutate({ token });
  };

  if (!token) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-[#F5F0EB]">
        <div className="text-center p-8">
          <XCircle size={48} className="mx-auto text-red-500 mb-4" />
          <h2 className="text-xl font-bold text-[#4A1525] mb-2">Invalid Invite Link</h2>
          <p className="text-gray-600 text-sm">This invite link is missing a token. Please request a new invite.</p>
        </div>
      </div>
    );
  }

  if (inviteLoading || authLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-[#F5F0EB]">
        <Loader2 size={32} className="animate-spin text-[#4A1525]" />
      </div>
    );
  }

  if (inviteError || !invite) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-[#F5F0EB]">
        <div className="text-center p-8 max-w-sm">
          <XCircle size={48} className="mx-auto text-red-500 mb-4" />
          <h2 className="text-xl font-bold text-[#4A1525] mb-2">Invite Not Found</h2>
          <p className="text-gray-600 text-sm">This invite link is invalid or has already been used. Please request a new invite from your campaign manager.</p>
        </div>
      </div>
    );
  }

  if (invite.acceptedAt) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-[#F5F0EB]">
        <div className="text-center p-8 max-w-sm">
          <CheckCircle size={48} className="mx-auto text-green-500 mb-4" />
          <h2 className="text-xl font-bold text-[#4A1525] mb-2">Invite Already Accepted</h2>
          <p className="text-gray-600 text-sm mb-4">This invite has already been used. If you need access, please contact your campaign manager.</p>
          <Button onClick={() => navigate("/")} className="bg-[#4A1525] text-white">Go to Dashboard</Button>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-[#F5F0EB]">
      <div className="bg-white border border-gray-200 rounded-lg shadow-lg p-8 max-w-sm w-full mx-4">
        <div className="text-center mb-6">
          <div className="w-16 h-16 rounded-full bg-[#4A1525] flex items-center justify-center mx-auto mb-4">
            <Users size={28} className="text-white" />
          </div>
          <h1 className="text-2xl font-bold text-[#4A1525] mb-1">Campaign Invite</h1>
          <p className="text-gray-500 text-sm">INEC Campaign Intelligence Platform</p>
        </div>

        <div className="bg-[#F5F0EB] rounded-lg p-4 mb-6">
          <p className="text-sm text-gray-700 mb-1">You've been invited as:</p>
          <p className="font-bold text-[#4A1525] text-lg capitalize">{invite.role}</p>
          <p className="text-xs text-gray-500 mt-1">Invited to: {invite.email}</p>
        </div>

        {!isAuthenticated ? (
          <div className="space-y-3">
            <p className="text-sm text-gray-600 text-center">You need to sign in to accept this invite.</p>
            <Button
              className="w-full bg-[#4A1525] hover:bg-[#3a1020] text-white"
              onClick={() => startLogin()}
            >
              Sign In to Accept Invite
            </Button>
            <p className="text-xs text-gray-400 text-center">After signing in, you'll be redirected back to accept.</p>
          </div>
        ) : (
          <div className="space-y-3">
            <Button
              className="w-full bg-[#008751] hover:bg-[#006B40] text-white"
              onClick={handleAccept}
              disabled={confirmMut.isPending}
            >
              {confirmMut.isPending ? <><Loader2 size={14} className="animate-spin mr-2"/>Joining…</> : <><CheckCircle size={14} className="mr-2"/>Accept & Join Campaign</>}
            </Button>
            <Button variant="ghost" className="w-full text-gray-500 text-sm" onClick={() => navigate("/")}>
              Decline
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}
