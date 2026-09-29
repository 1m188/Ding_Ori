using System;
using System.Reflection;

namespace OriTrainerDLL.Features
{
    // 无限二段跳：开局无二段跳能力也能空中无限二段跳。
    //
    // 分两层：
    //   · OnGameFixedUpdate（主线程）：授予基础二段跳能力 + 实例化二段跳组件。
    //     SeinDoubleJump 组件由 SeinPrefabFactory.EnsureRightPrefabsAreThereForAbilities()
    //     用 Object.Instantiate 实例化（Unity API 仅主线程），且该方法只读 HasAbility
    //     不授予能力，所以必须先写 HasAbility=true，再调用实例化。
    //   · hook CanDoubleJump 恒 true：免去每帧写 m_numberOfJumpsAvailable /
    //     m_remainingLockTime。判定不再看次数与锁，天然无限。
    //
    // ---- 为什么必须保留组件解锁，而不能只 hook 判定 ----
    // SeinController.PerformJump 的二段跳分支要求 CharacterState.IsActive(DoubleJump)
    // = (bool)DoubleJump && DoubleJump.Active，且 PerformDoubleJump() 是实例方法，
    // 必须有 SeinDoubleJump 实例才能调用。组件未实例化（开局无能力）时 Sein.Abilities
    // .DoubleJump 恒为 null，既不进判断也没法凭空调二段跳，光 hook 判定没用。
    // （曾在 hook 整个 PerformJump 时踩过此坑：去掉授予后 EnsureRightPrefabs... 只读
    //  false 的 HasAbility，组件永不实例化，表现如同没开。）
    //
    // ---- 组件解锁只需主线程 ----
    // 组件解锁需要 Object.Instantiate（Unity API，仅主线程），所以挂
    // Game.Events.Scheduler.OnGameFixedUpdate（游戏主线程每帧回调）。它只做
    // HasAbility + EnsureRightPrefabsAreThereForAbilities，不做字段写入（字段由 hook 免除）。
    public static class InfiniteDoubleJump
    {
        private static readonly BindingFlags PublicInstance =
            BindingFlags.Public | BindingFlags.Instance;

        private static Hooks.Hook _hook;

        private static Action _unlockHook; // OnGameFixedUpdate 引用，Stop 时注销

        public static void Start()
        {
            if (_hook != null && _unlockHook != null) return; // 幂等：重复 Start 不重复挂载

            // 先取 Scheduler：游戏未就绪时直接抛，避免 hook 已挂上而解锁钩子未挂的半挂载态
            // （否则下次 Start 才在 _unlockHook==null 分支补齐，界面显示开启却未完全生效）。
            GameScheduler scheduler = Game.Events.Scheduler ?? throw new Exception("GameScheduler 尚未就绪（游戏未启动完成），功能无法挂载");

            // hook CanDoubleJump：属性 getter，走 GetProperty+GetGetMethod 避免 specialname 坑
            PropertyInfo prop = typeof(SeinDoubleJump).GetProperty("CanDoubleJump", PublicInstance);
            MethodInfo target = (prop?.GetGetMethod(true)) ?? throw new Exception("SeinDoubleJump.CanDoubleJump 与预期不符，功能无法工作");

            if (_hook == null)
                _hook = Hooks.Hook.Apply(target,
                    typeof(InfiniteDoubleJump).GetMethod("OnCanDoubleJump",
                        BindingFlags.NonPublic | BindingFlags.Static));

            // 组件解锁挂在游戏主线程每帧回调
            if (_unlockHook == null)
            {
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

            _hook?.Dispose();
            _hook = null;
        }

        // 由游戏主线程每帧调用：授予能力 + 实例化组件，保证 Sein.Abilities.DoubleJump 可用。
        private static void OnGameFixedUpdate()
        {
            try
            {
                SeinCharacter sein = Game.Characters.Sein;
                if (sein == null) return;

                PlayerAbilities playerAbilities = sein.PlayerAbilities;
                if (playerAbilities == null) return;

                // 授予基础二段跳能力（EnsureRightPrefabs... 只读 HasAbility，须先写）
                if (playerAbilities.DoubleJump != null)
                    playerAbilities.DoubleJump.HasAbility = true;

                // 实例化二段跳组件（EnsureRightPrefabs... 按 HasAbility 补建，主体线程）
                sein.Prefabs?.EnsureRightPrefabsAreThereForAbilities();
            }
            catch { }
        }

        // hook CanDoubleJump：恒可二段跳（不看次数/锁，天然无限）。
        // this 以第一参数传入。
        private static bool OnCanDoubleJump(SeinDoubleJump jump)
        {
            return true;
        }
    }
}