import { VoiceConnectionStatus, entersState, type VoiceConnection } from "@discordjs/voice";

// A disconnected connection remains registered with Discord's voice library. Reusing
// that object does not send another join request; explicitly rejoin and await Ready.
export async function ensureConnectionReady(connection: VoiceConnection, channelId: string, timeoutMs: number): Promise<void> {
  if (connection.state.status === VoiceConnectionStatus.Disconnected) {
    if (!connection.rejoin({ channelId })) throw new Error("The Discord voice connection could not rejoin.");
  }
  if (connection.state.status === VoiceConnectionStatus.Destroyed) {
    throw new Error("The Discord voice connection was closed.");
  }
  try {
    await entersState(connection, VoiceConnectionStatus.Ready, timeoutMs);
  } catch (cause) {
    throw new Error("The Discord voice connection did not become ready.", { cause });
  }
}
