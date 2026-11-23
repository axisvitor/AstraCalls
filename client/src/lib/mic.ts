/** Erro de getUserMedia por microfone bloqueado/ausente. */
export const micDenied = (e: unknown): boolean => {
  const name = (e as { name?: string })?.name ?? "";
  return name === "NotAllowedError" || name === "SecurityError" || name === "NotFoundError";
};

export const MIC_DENIED_MSG =
  "Microfone bloqueado ou não encontrado. Permita o microfone no cadeado ao lado do endereço do site e tente de novo.";
