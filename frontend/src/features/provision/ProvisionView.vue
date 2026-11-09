<script setup lang="ts">
/**
 * FEAT-059. What a governed database needs before a release can reach it.
 *
 * Read first, act second, and the page is laid out that way: a name, what the
 * server says about it, and then buttons for the gaps. The buttons appear only
 * for gaps, because an action for something already done is an invitation to
 * find out what it does.
 *
 * No database is removed from here. There is no drop, no revoke and no rename
 * of one — undoing that is a DBA at a psql prompt, deliberately.
 *
 * Roles are the exception, below. FEAT-061: the login a service connects as is
 * created here with a password shown once and kept nowhere, rotated here, and
 * a role Avior created is dropped here — by name, never an owner role.
 *
 * FEAT-076 adds the database layer to the check: whether each declared role
 * may connect, and whether the write path reaches the database — each with the
 * one button that closes the gap, and neither with a way back.
 */
import { computed, ref } from 'vue'

import { describeWriteGaps, nameProblem, useProvisioning } from './useProvisioning'
import PasswordReveal from './PasswordReveal.vue'
import RoleDialog from './RoleDialog.vue'
import RolesMap from './RolesMap.vue'
import {
  useRoles,
  type DatabaseGrantSummary,
  type ProvisionedLogin,
  type ProvisionedRole,
} from './useRoles'
import {
  VBanner,
  VButton,
  VDialog,
  VEmpty,
  VField,
  VFilterBar,
  VInput,
  VLoader,
  VPageHeader,
  VSkeleton,
  VTable,
} from '@/components'
import { describeFailure } from '@/shared/api'

const provisioning = useProvisioning()
const roles = useRoles()

const chosenRole = computed(
  () => roles.roles.value.find((role) => role.name === roles.chosen.value) ?? null,
)

const loginName = ref('')
const memberOf = ref<string[]>([])
const newLoginOpen = ref(false)
const rotating = ref<string | null>(null)
const rotated = ref<ProvisionedLogin | null>(null)
const dropping = ref<string | null>(null)

const loginProblem = computed(() => (loginName.value === '' ? null : nameProblem(loginName.value)))
const canCreateLogin = computed(
  () => loginName.value.trim() !== '' && loginProblem.value === null && !roles.busy.value,
)

function openNewLogin(): void {
  loginName.value = ''
  memberOf.value = []
  roles.dismissMinted()
  newLoginOpen.value = true
}

function closeNewLogin(): void {
  newLoginOpen.value = false
  roles.dismissMinted()
}

async function createLogin(): Promise<void> {
  await roles.createLogin(loginName.value, memberOf.value)
}

async function confirmRotate(): Promise<void> {
  if (rotating.value === null) return
  await roles.rotate(rotating.value)
  rotated.value = roles.minted.value
  rotating.value = null
}

function closeRotate(): void {
  rotating.value = null
  rotated.value = null
  roles.dismissMinted()
}

async function confirmDrop(): Promise<void> {
  if (dropping.value === null) return
  const name = dropping.value
  dropping.value = null
  if (roles.chosen.value === name) roles.close()
  await roles.drop(name)
}

/** One line for what a role may do in one database. */
function describeGrant(grant: DatabaseGrantSummary): string {
  const parts: string[] = []
  if (grant.select > 0) parts.push(`SELECT on ${grant.select}/${grant.tables} tables`)
  const writes = [
    grant.insert > 0 ? 'INSERT' : '',
    grant.update > 0 ? 'UPDATE' : '',
    grant.delete > 0 ? 'DELETE' : '',
  ].filter(Boolean)
  if (writes.length > 0) {
    parts.push(`${writes.join('/')} on ${Math.max(grant.insert, grant.update, grant.delete)}`)
  }
  return parts.length > 0 ? parts.join(' · ') : 'CONNECT only'
}

/** The grants of a group, compressed for the picker. */
function describeGrants(group: ProvisionedRole): string {
  if (group.grants.length === 0) return 'grants nothing yet'
  return group.grants.map((grant) => `${grant.database}: ${describeGrant(grant)}`).join(' · ')
}

const bannerText = computed(() => {
  const failure = provisioning.failure.value
  return failure === null ? null : describeFailure(failure)
})

const typedProblem = computed(() =>
  provisioning.database.value === '' ? null : nameProblem(provisioning.database.value),
)

/** The roles a submission declared that the server does not hold. */
const absentRoles = computed(
  () => provisioning.preconditions.value?.roles.filter((role) => !role.exists) ?? [],
)

/** The declared roles that exist and may not connect. FEAT-076 AC-13. */
const unconnectable = computed(
  () => provisioning.preconditions.value?.roles.filter((role) => role.canConnect === false) ?? [],
)

const writes = computed(() => provisioning.preconditions.value?.writes ?? null)
const writerMissing = computed(() => writes.value?.missing.includes('writer_role') ?? false)

const hint = 'm-0 text-sm text-fg-muted'
const code = 'font-mono text-sm'

const dialogActions = 'mt-4 flex justify-end gap-2'
</script>

<template>
  <section class="flex h-full min-h-0 flex-col gap-4 overflow-y-auto">
    <VPageHeader
      icon="server"
      title="Provisioning"
      :busy="provisioning.busy.value"
      busy-label="Asking the server"
    />

    <VBanner v-if="bannerText" tone="danger">{{ bannerText }}</VBanner>

    <!--
      The bar carries its own submit button, so this passes it a label rather
      than adding a second one beside it. It had two: "Check", which read the
      preconditions, and the bar's own "Apply", which submitted a form nothing
      was listening to and did nothing at all — two buttons, one of them inert,
      on a page whose whole job is to say what is missing.
    -->
    <VFilterBar label="Check" :busy="provisioning.busy.value" @submit="provisioning.load()">
      <VField label="Database" for="provision-database" wide>
        <VInput
          id="provision-database"
          v-model="provisioning.database.value"
          placeholder="shop"
          autocomplete="off"
        />
      </VField>
      <VField label="Roles the migration grants to" for="provision-roles" wide>
        <VInput
          id="provision-roles"
          v-model="provisioning.roles.value"
          placeholder="role_shop"
          autocomplete="off"
        />
      </VField>
    </VFilterBar>

    <p v-if="typedProblem" :class="hint">{{ typedProblem }}</p>

    <!-- Checking reaches the governed server, so it is the slow one on this
         page. Before the empty state, which says the opposite: that nothing has
         been asked for yet. -->
    <VSkeleton
      v-if="provisioning.busy.value && provisioning.preconditions.value === null"
      :lines="4"
      label="Asking the server"
    />

    <VEmpty v-else-if="provisioning.preconditions.value === null">
      Name a database to see what it is missing.
    </VEmpty>

    <template v-else-if="provisioning.preconditions.value !== null">
      <VBanner v-if="provisioning.ready.value" tone="info">
        Everything a release needs is there.
      </VBanner>

      <VTable variant="report" caption-visible class="[&>caption]:eyebrow">
        <template #caption>
          What
          {{ provisioning.preconditions.value.database }}
          has
        </template>
        <tr>
          <th scope="row">Database</th>
          <td>{{ provisioning.preconditions.value.databaseExists ? 'present' : 'missing' }}</td>
          <td>
            <VButton
              v-if="!provisioning.preconditions.value.databaseExists"
              variant="primary"
              :disabled="provisioning.busy.value"
              @click="provisioning.createDatabase()"
            >
              Create it
            </VButton>
          </td>
        </tr>
        <tr>
          <th scope="row">{{ provisioning.preconditions.value.ownerRole }}</th>
          <td>{{ provisioning.preconditions.value.ownerRoleExists ? 'present' : 'missing' }}</td>
          <td>
            <!--
                No button of its own. The owner role is created with the
                database, because a database whose owner role nobody is a member
                of takes a migration's CREATE and refuses its ALTER.
              -->
          </td>
        </tr>
        <tr>
          <th scope="row">Deploy credential owns the schema</th>
          <td>{{ provisioning.preconditions.value.deployerOwnsSchema ? 'yes' : 'no' }}</td>
          <td></td>
        </tr>
        <tr v-for="role in provisioning.preconditions.value.roles" :key="role.name">
          <th scope="row">{{ role.name }}</th>
          <td>
            {{ role.exists ? 'present' : 'missing' }}
            <template v-if="role.canConnect === false">
              · <span class="text-warn">cannot connect</span>
            </template>
            <template v-else-if="role.canConnect">· may connect</template>
          </td>
          <td>
            <VButton
              v-if="!role.exists"
              :disabled="provisioning.busy.value"
              @click="provisioning.createRole(role.name)"
            >
              Create it
            </VButton>
            <VButton
              v-else-if="role.canConnect === false"
              :disabled="provisioning.busy.value"
              @click="provisioning.grantConnect(role.name)"
            >
              Grant connect
            </VButton>
          </td>
        </tr>
        <!-- FEAT-076. The write path, about the writer Avior is configured with
             and no other: there is nothing here to type a password into. -->
        <tr v-if="writes">
          <th scope="row">Write path · {{ writes.writerRole }}</th>
          <td>
            <template v-if="writes.prepared">prepared</template>
            <span v-else-if="writerMissing" class="text-warn">
              {{ writes.writerRole }} is not on the server — deploy/store/bootstrap.sql creates it,
              once per cluster
            </span>
            <template v-else>
              <span class="text-warn">not prepared</span> · missing
              {{ describeWriteGaps(writes) }}
            </template>
          </td>
          <td>
            <VButton
              v-if="!writes.prepared && !writerMissing"
              :disabled="provisioning.busy.value"
              @click="provisioning.prepareWrites()"
            >
              Prepare for writes
            </VButton>
          </td>
        </tr>
      </VTable>

      <p v-if="unconnectable.length > 0" :class="hint">
        A login reaches a database through a group only once the group may connect to it. Avior
        revokes CONNECT from everybody when it creates a database, so each group is granted it here.
      </p>

      <p v-if="absentRoles.length > 0" :class="hint">
        A role a migration grants to belongs in the project that owns the schema, and reaches
        production with it. Creating it here unblocks a release against a project that does not yet
        ship its own.
      </p>
    </template>

    <!-- FEAT-061. The login a service connects as, and the roles Avior made. -->
    <section class="mt-2 flex flex-col gap-3.5 border-t border-line px-5 pt-4 pb-8">
      <div class="flex items-center justify-between">
        <h2 class="m-0 eyebrow">Roles</h2>
        <VButton variant="primary" :disabled="roles.busy.value" @click="openNewLogin()">
          New login
        </VButton>
      </div>

      <VFilterBar label="Search" :busy="roles.busy.value" @submit="roles.load()">
        <VField label="Database" for="roles-database" wide>
          <VInput
            id="roles-database"
            v-model="roles.database.value"
            placeholder="shop"
            autocomplete="off"
          />
        </VField>
      </VFilterBar>

      <VBanner v-if="roles.failure.value" tone="danger">
        {{ describeFailure(roles.failure.value) }}
      </VBanner>

      <div v-if="roles.searching.value && !roles.loaded.value" class="flex justify-center py-10">
        <VLoader size="section" label="Searching the roles" />
      </div>
      <VEmpty v-else-if="!roles.loaded.value">
        Name a database to see the roles that reach it.
      </VEmpty>
      <VEmpty v-else-if="roles.roles.value.length === 0">
        No role Avior created concerns {{ roles.database.value }}.
      </VEmpty>
      <!-- FEAT-076 AC-17. The roles about the database in three columns, each
           card opening the dialog below. -->
      <RolesMap
        v-else
        :roles="roles.roles.value"
        :database="roles.loadedDatabase.value"
        :chosen="roles.chosen.value"
        @choose="roles.open($event)"
      />

      <RoleDialog
        :role="chosenRole"
        :access="roles.access.value"
        :failure="roles.accessFailure.value"
        :reading="roles.reading.value"
        :busy="roles.busy.value"
        @close="roles.close()"
        @rotate="rotating = $event"
        @drop="dropping = $event"
        @grant-connect="roles.grantConnect($event)"
      />

      <!-- New login. The password takes the dialog over once it exists, and
           closing the dialog is the one way past it: the value lives nowhere
           else. -->
      <VDialog :open="newLoginOpen" title="New login" @close="closeNewLogin()">
        <template v-if="roles.minted.value === null">
          <form class="flex flex-col gap-3 [&>div]:basis-auto" @submit.prevent="createLogin()">
            <p :class="hint">
              A login has no privileges of its own: it gets what the groups it joins hold — the
              <code :class="code">role_&lt;service&gt;</code> a project's migrations grant to.
            </p>
            <VField label="Name" for="login-name">
              <VInput
                id="login-name"
                v-model="loginName"
                placeholder="shop_app"
                autocomplete="off"
              />
            </VField>
            <fieldset class="m-0 flex flex-col gap-2 border-0 p-0">
              <legend
                class="mb-1.5 p-0 text-xs font-semibold tracking-label text-fg-soft uppercase"
              >
                Member of
              </legend>
              <p v-if="roles.groups.value.length === 0" :class="hint">
                No group role reaches {{ roles.database.value }} yet. Create
                <code :class="code">role_&lt;service&gt;</code> above, or let the project's
                migration ship it.
              </p>
              <label
                v-for="group in roles.groups.value"
                :key="group.name"
                class="flex items-start gap-2 text-md"
              >
                <input
                  v-model="memberOf"
                  class="mt-[--spacing(0.8)]"
                  type="checkbox"
                  :value="group.name"
                />
                <span>
                  <code :class="code">{{ group.name }}</code>
                  <small class="block text-xs text-fg-muted">{{ describeGrants(group) }}</small>
                </span>
              </label>
            </fieldset>
            <p v-if="loginProblem" :class="hint">{{ loginProblem }}</p>
            <p v-else-if="memberOf.length === 0" class="m-0 text-sm text-warn">
              No group selected: this login will connect to nothing until a migration grants it
              something.
            </p>
            <div :class="dialogActions">
              <VButton variant="bare" @click="closeNewLogin()">Cancel</VButton>
              <VButton type="submit" variant="primary" :disabled="!canCreateLogin">
                Create login
              </VButton>
            </div>
          </form>
        </template>
        <PasswordReveal v-else :login="roles.minted.value" @done="closeNewLogin()" />
      </VDialog>

      <!-- New password: a confirmation first, then the same one-time reveal. -->
      <VDialog
        :open="rotating !== null || rotated !== null"
        :title="rotated ? 'New password' : 'Replace the password?'"
        @close="closeRotate()"
      >
        <template v-if="rotated === null">
          <p>
            <code :class="code">{{ rotating }}</code> gets a new password. Connections already open
            keep working; the next one needs the new password, so update OpenBao before the service
            restarts.
          </p>
          <div :class="dialogActions">
            <VButton variant="bare" @click="closeRotate()">Keep the current one</VButton>
            <VButton variant="primary" :disabled="roles.busy.value" @click="confirmRotate()">
              Replace it
            </VButton>
          </div>
        </template>
        <PasswordReveal v-else :login="rotated" @done="closeRotate()" />
      </VDialog>

      <VDialog :open="dropping !== null" title="Drop this role?" @close="dropping = null">
        <p>
          <code :class="code">{{ dropping }}</code> is removed from the server. Nothing it owns is
          touched: if it still owns something, PostgreSQL refuses and says what.
        </p>
        <div :class="dialogActions">
          <VButton variant="bare" @click="dropping = null">Keep it</VButton>
          <VButton variant="primary" @click="confirmDrop()">Drop</VButton>
        </div>
      </VDialog>
    </section>
  </section>
</template>
