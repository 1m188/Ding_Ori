using System;
using System.Reflection;

namespace OriTrainerDLL.Features
{
    // 无限冲刺（完全版：基础冲刺 + 空中冲刺 + 无次数/冷却限制）。
    //
    // 分两层：
    //   · hook SeinDashAttack.CanPerformNormalDash：即判可普通冲刺，消除次数与冷却。
    //     原实现每帧 ResetDashLimit()（清 m_hasDashed）+ 清 m_lastDashTime，改成 hook
    //     恒可后彻底删除，不再读这两个字段。
    //   · OnGameFixedUpdate（主线程）：授予基础冲刺 + 空中冲刺能力并实例化冲刺组件。
    //
    // ---- 为什么 replacement 保留 !AgainstWall() ----
    // 贴墙时若 CanPerformNormalDash 恒 true，会抢在 CanWallDash 之前命中普通冲刺，
    // 墙冲刺（SeinDashAttack.CanWallDash → PerformWallDash）被遮蔽，且普通冲刺贴墙会被
    // UpdateDashing 的 AgainstWall 停机逻辑直接刹停，观感变差。保留 !AgainstWall() 让
    // 贴墙时 normal dash 仍判 false，落到墙 dash 分支，保留原有墙冲刺。
    //
    // ---- 为什么必须保留组件解锁，而不能只 hook 判定 ----
    // CanPerformNormalDash 是 SeinDashAttack 实例方法，调用前提是组件已实例化
    // （Sein.Abilities.Dash 非 null）。组件由 SeinPrefabFactory.EnsureRightPrefabsAreThereForAbilities()
    // 用 Object.Instantiate 实例化（Unity API 仅主线程），且该方法只读 HasAbility 不授予，
    // 所以必须先写 HasAbility=true 再调实例化。开局无冲刺能力时组件为 null，光 hook 无效
    // （同 InfiniteDoubleJump 的坑）。
    //
    // ---- 能力解锁点 ----
    // 玩家任一时刻开启本功能都应获得完整冲刺能力，故两项都授予：
    //   ① PlayerAbilities.Dash.HasAbility       —— 基础冲刺
    //   ② PlayerAbilities.AirDash.HasAbility    —— 空中冲刺技能（无对应 SeinNestedPrefab，
    //      只是被 HasAirDashSkill()/CanWallDash() 读取的开关，无需实例化）
    //   ③ EnsureRightPrefabsAreThereForAbilities() —— 实例化基础冲刺组件
    // 组件一旦实例化 Sein.Abilities.Dash 即持续存在，hook 恒可常驻生效。
    //
    // ⚠ 存档影响：PlayerAbilities.Serialize() 会把 HasAbility 写进存档，开启后存一次档，
    // 冲刺能力即成为存档既有能力，关闭也收不回。
    public static class InfiniteDash
    {
        private static Hooks.Hook _hook;

        private static Action _unlockHook; // OnGameFixedUpdate 引用，Stop 时注销

        public static void Start()
        {
            if (_hook != null && _unlockHook != null) return; // 幂等：重复 Start 不重复挂载

            // hook CanPerformNormalDash：public 实例方法，直接 GetMethod
            MethodInfo target = typeof(SeinDashAttack).GetMethod("CanPerformNormalDash",
                BindingFlags.Public | BindingFlags.Instance);
            if (target == null)
                throw new Exception("SeinDashAttack.CanPerformNormalDash 与预期不符，功能无法工作");

            if (_hook == null)
                _hook = Hooks.Hook.Apply(target,
                    typeof(InfiniteDash).GetMethod("OnCanPerformNormalDash",
                        BindingFlags.NonPublic | BindingFlags.Static));

            // 组件解锁挂在游戏主线程每帧回调
            if (_unlockHook == null)
            {
                GameScheduler scheduler = Game.Events.Scheduler ?? throw new Exception("GameScheduler 尚未就绪（游戏未启动完成），功能无法挂载");
                _unlockHook = OnGameFixedUpdate;
                scheduler.OnGameFixedUpdate.Add(_unlockHook);
            }
        }

        public static void Stop()
        {
            if (_unlockHook != null)
            {
                Game.Events.Scheduler.OnGameFixedUpdate.Remove(_unlockHook);
                _unlockHook = null;
            }
            if (_hook != null)
            {
                _hook.Dispose();
                _hook = null;
            }
        }

        // 由游戏主线程每帧调用：授予能力 + 实例化组件，保证 Sein.Abilities.Dash 可用。
        private static void OnGameFixedUpdate()
        {
            try
            {
                SeinCharacter sein = Game.Characters.Sein;
                if (sein == null) return;

                PlayerAbilities playerAbilities = sein.PlayerAbilities;
                if (playerAbilities == null) return;

                // ① 基础冲刺能力（EnsureRightPrefabs... 只读 HasAbility，须先写）
                if (playerAbilities.Dash != null)
                    playerAbilities.Dash.HasAbility = true;

                // ② 空中冲刺技能（无实例化需求，纯开关）
                if (playerAbilities.AirDash != null)
                    playerAbilities.AirDash.HasAbility = true;

                // ③ 实例化冲刺组件（EnsureRightPrefabs... 按 HasAbility 补建，主线程）
                sein.Prefabs?.EnsureRightPrefabsAreThereForAbilities();
            }
            catch { }
        }

        // hook CanPerformNormalDash：保留贴墙禁止，让贴墙时落到墙 dash 分支；否则恒可。
        // 已不读 m_hasDashed / DashHasCooledDown，天然无限 + 无冷却。this 以第一参数传入。
        private static bool OnCanPerformNormalDash(SeinDashAttack dash)
        {
            return dash != null && !dash.AgainstWall();
        }
    }
}