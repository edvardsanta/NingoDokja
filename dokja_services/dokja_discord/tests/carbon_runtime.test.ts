import test from "node:test";
import assert from "node:assert/strict";

import { startDiscordRuntime } from "../runtime/carbon_runtime.js";

test("startDiscordRuntime wires commands, message listener, and guild-scoped deploy", async () => {
  let deployCalled = false;
  let fetchUserCalled = false;
  let waitCalled = false;
  let capturedClientOptions: any;
  let capturedHandlers: any;

  const fakeClient = {
    listeners: [] as any[],
    eventHandler: {
      handleEvent() {
        return true;
      },
    },
    async handleDeployRequest() {
      deployCalled = true;
    },
    async fetchUser(_id: string) {
      fetchUserCalled = true;
      return { id: "bot-1" };
    },
    getPlugin(_id: string) {
      return {
        disconnect() {},
      };
    },
  };

  await startDiscordRuntime(
    {
      token: "token",
      applicationId: "app-1",
      devGuildId: "guild-1",
      commands: [
        {
          name: "chat",
          description: "chat",
          async handle() {},
        },
      ],
      onMessage: async () => {},
    },
    {
      fetchApplicationId: async () => ({ id: "app-1" }),
      createClient: ({ applicationId, token, devGuildId, commands }) => {
        capturedClientOptions = { applicationId, token, devGuildId };
        capturedHandlers = { commands };
        return fakeClient as any;
      },
      getGatewayEmitter: () => undefined,
      waitForGatewayStop: async () => {
        waitCalled = true;
      },
    },
  );

  assert.deepEqual(capturedClientOptions, {
    applicationId: "app-1",
    token: "token",
    devGuildId: "guild-1",
  });
  assert.equal(capturedHandlers.commands.length, 1);
  assert.equal(deployCalled, true);
  assert.equal(fetchUserCalled, true);
  assert.equal(fakeClient.listeners.length, 1);
  assert.equal(waitCalled, true);
});

test("startDiscordRuntime forwards interactive definitions into the client factory", async () => {
  let capturedClientOptions: any;

  const fakeClient = {
    listeners: [] as any[],
    eventHandler: {
      handleEvent() {
        return true;
      },
    },
    async handleDeployRequest() {},
    async fetchUser(_id: string) {
      return { id: "bot-1" };
    },
    getPlugin(_id: string) {
      return {
        disconnect() {},
      };
    },
  };

  const button = {
    customId: "button-1",
    label: "Button",
    async handle() {},
  };
  const select = {
    customId: "select-1",
    options: [{ label: "One", value: "1" }],
    async handle() {},
  };
  const modal = {
    customId: "modal-1",
    title: "Modal",
    fields: [{ customId: "prompt", label: "Prompt" }],
    async handle() {},
  };

  await startDiscordRuntime(
    {
      token: "token",
      applicationId: "app-1",
      commands: [
        {
          name: "chat",
          description: "chat",
          async handle() {},
        },
      ],
      buttons: [button],
      selects: [select],
      modals: [modal],
      onMessage: async () => {},
    },
    {
      fetchApplicationId: async () => ({ id: "app-1" }),
      createClient: (params) => {
        capturedClientOptions = params;
        return fakeClient as any;
      },
      getGatewayEmitter: () => undefined,
      waitForGatewayStop: async () => {},
    },
  );

  assert.equal(capturedClientOptions.buttons?.[0], button);
  assert.equal(capturedClientOptions.selects?.[0], select);
  assert.equal(capturedClientOptions.modals?.[0], modal);
});
