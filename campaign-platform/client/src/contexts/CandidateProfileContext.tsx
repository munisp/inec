import { createContext, useContext, ReactNode } from "react";
import { trpc } from "@/lib/trpc";

export type MemberRole = "owner" | "manager" | "viewer";

export type CandidateProfile = {
  id: number;
  candidateName: string;
  partyName: string | null;
  partyColor: string | null;
  stateCode: string | null;
  stateName: string | null;
  office: "President" | "Governor" | "Senator" | "House" | "LGA" | null;
  religion: string | null;
  gender: string | null;
  geopoliticalZone: string | null;
  isActive: boolean | null;
};

type ProfileContextValue = {
  profile: CandidateProfile | null;
  profileId: number | null;
  isLoading: boolean;
  refetch: () => void;
  memberRole: MemberRole;
  canEdit: boolean;
  canDelete: boolean;
};

const CandidateProfileContext = createContext<ProfileContextValue>({
  profile: null,
  profileId: null,
  isLoading: true,
  refetch: () => {},
  memberRole: "owner",
  canEdit: true,
  canDelete: true,
});

export function CandidateProfileProvider({ children }: { children: ReactNode }) {
  const { data, isLoading, refetch } = trpc.profile.get.useQuery(undefined, {
    staleTime: 30_000,
    refetchOnWindowFocus: false,
  });

  const profileId = data?.id ?? null;
  const { data: roleData } = trpc.team.myRole.useQuery(
    { profileId: profileId! },
    { enabled: !!profileId, staleTime: 60_000 }
  );
  const memberRole: MemberRole = (roleData as MemberRole | undefined) ?? "owner";

  return (
    <CandidateProfileContext.Provider
      value={{
        profile: (data as CandidateProfile | null | undefined) ?? null,
        profileId,
        isLoading,
        refetch,
        memberRole,
        canEdit: memberRole !== "viewer",
        canDelete: memberRole === "owner",
      }}
    >
      {children}
    </CandidateProfileContext.Provider>
  );
}

export function useCandidateProfile() {
  return useContext(CandidateProfileContext);
}
