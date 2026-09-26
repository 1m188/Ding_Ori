using System;
using System.Reflection;

namespace OriTrainerDLL.FeaturesDE
{
    // 无限冲刺（完全版：基础冲刺 + 空中冲刺 + 无次数/冷却限制）。
    //
    // 游戏的门槛在 SeinDashAttack.CanPerformNormalDash：
    //     if (!HasAirDashSkill() && !IsOnGround) return false;
    //     return !AgainstWall() && DashHasCooledDown && !m_hasDashed;
    // 其中 HasAirDashSkill() = PlayerAbilities.AirDash.HasAbility。
    //
    // ---- 为什么必须挂主线程钩子，而不能像其他功能那样用定时器 ----
    //
    // 只写 HasAbility 是**没用的**：SeinPrefabFactory.Awake() 只自动实例化 13 个组件
    // （Carry/Crouch/Fall/Jump/PushAgainstWall/Run/Idle/StandingOnEdge/Swimming/
    // SoulFlame/GrabPushPull/SpiritFlame/PickupProcessor），**Dash 不在其中**。
    // 而 SeinAbilities.Dash 全程序只有 SeinDashAttack.SetReferenceToSein() 会写，
    // 后者由 SeinNestedPrefab.Instantiate() → SeinCharacter.MakeBelongToSein() →
    // BroadcastMessage("SetReferenceToSein") 回填。未实例化时 SeinAbilities.Dash
    // 恒为 null —— 此时无论怎么写次数与冷却都毫无效果（golang 版即卡在这里，
    // 它状态栏那句"组件待读档或买技能后实例化"就是在承认这个缺陷）。
    //
    // 实例化的唯一入口是 SeinPrefabFactory.EnsureRightPrefabsAreThereForAbilities()，
    // 它内部走 Object.Instantiate 创建 GameObject —— Unity API，**必须在主线程调用**
    // （从定时器线程调 Unity 对象创建不安全，参见此前 Application.Quit() 崩游戏的教训）。
    // 因此本功能挂到 GameController.FixedUpdate 里调用的 UberDelegate：
    //     Game.Events.Scheduler.OnGameFixedUpdate
    // 这是游戏自己的每帧主线程回调，官方 CheatsHandler 也用同样方式挂 OnGameReset。
    //
    // 频率足够：该回调每个 FixedUpdate 触发一次，而 m_hasDashed 的写入（PerformDash）
    // 与读取（CanPerformNormalDash）都发生在 SeinLogicCycle.FixedUpdate 内，同为固定步长，
    // 最坏情况差一帧，人手按键远达不到这个速度。
    //
    // ---- 三处覆盖 ----
    //   ① 授予基础冲刺 PlayerAbilities.Dash.HasAbility；
    //   ② 授予空中冲刺 PlayerAbilities.AirDash.HasAbility（它没有对应的 SeinNestedPrefab，
    //      只是个被 HasAirDashSkill()/CanWallDash() 读取的开关，不需要实例化任何东西）；
    //   ③ 调 EnsureRightPrefabsAreThereForAbilities() 实例化冲刺组件（关键的一步），
    //      再持续清 m_hasDashed 与 m_lastDashTime，解除次数与 0.4s 冷却。
    //
    // ③ 同时修好了场景切换：ApplyInitialValues() 每次进场景都会按场景元数据把
    // Dash.HasAbility 写回，进而让 EnsureRightPrefabs 把组件 Destroy 掉；本功能每帧
    // 重新置位并重新实例化，所以不再需要"读档或买技能"才能恢复。
    //
    // ⚠ 存档影响：PlayerAbilities.Awake() 把 AirDash 收进 Abilities[41]（共 43 项），
    // 而 PlayerAbilities.Serialize() 遍历该数组、把每项 HasAbility 写进存档。
    // 开启本功能并让游戏存一次档之后，空中冲刺即成为存档中的既有能力，关闭功能也收不回。
    //
    // 不调 PlayerAbilities.SetAbility(AbilityType.Dash, true)：它内部会连带调
    // EnsureRightPrefabs，与本功能重复；直接写字段更直白。
    public static class InfiniteDash
    {
        private static readonly BindingFlags Private =
            BindingFlags.NonPublic | BindingFlags.Instance;

        private static FieldInfo _fLastDashTime; // m_lastDashTime
        private static Action _hook;             // 保留引用以便 Stop 时注销

        public static void Start()
        {
            if (_hook != null) return; // 幂等：重复 Start 不重复挂载

            _fLastDashTime = typeof(SeinDashAttack).GetField("m_lastDashTime", Private);

            // 字段名对不上就直接失败（Loader 会记进错误日志），而不是每帧静默空转
            if (_fLastDashTime == null)
                throw new Exception("SeinDashAttack 的字段名与预期不符，功能无法工作");

            // Scheduler 由 GameController 持有，而 GameController.Awake 是单例守卫
            // （Instance 已存在则 Destroy 自身），所以该回调在整个进程内稳定可用。
            // 游戏尚未启动到 GameController 时取出会得到 null 或抛异常，直接报错更易排查。
            GameScheduler scheduler = Game.Events.Scheduler;
            if (scheduler == null)
                throw new Exception("GameScheduler 尚未就绪（游戏未启动完成），功能无法挂载");

            _hook = OnGameFixedUpdate;
            scheduler.OnGameFixedUpdate.Add(_hook);
        }

        public static void Stop()
        {
            if (_hook == null) return;

            Game.Events.Scheduler.OnGameFixedUpdate.Remove(_hook);
            _hook = null;

            // 刻意不还原 HasAbility（Dash 与 AirDash 都是）：
            // Dash 的 SeinNestedPrefab.IsInstantiated setter 置 false 时会 Destroy()
            // 掉能力组件，还原后再次开启无法只靠写标志位重建，会变成"关了再开就失效"。
            // 而 AirDash 已被写进存档（见文件头），还原也收不回来。
            // 副作用是关闭功能后仍保留普通冲刺与空中冲刺。
        }

        // 由游戏主线程每个 FixedUpdate 调用
        private static void OnGameFixedUpdate()
        {
            // 游戏回调里抛出的异常会顺着 GameController.FixedUpdate 冒到 Unity，
            // 后果不可预期，必须自己兜住
            try
            {
                // 主菜单/读档过程中 Sein 为 null，显式判空
                SeinCharacter sein = Game.Characters.Sein;
                if (sein == null) return;

                // ①② 能力开关（CharacterAbility.HasAbility 是普通类字段，直接写）
                PlayerAbilities playerAbilities = sein.PlayerAbilities;
                if (playerAbilities == null) return;

                if (playerAbilities.Dash != null)
                    playerAbilities.Dash.HasAbility = true;

                if (playerAbilities.AirDash != null)
                    playerAbilities.AirDash.HasAbility = true;

                // ③ 实例化冲刺组件（本功能唯一需要主线程的一步）。
                // 它按上面两个开关把 SeinNestedPrefab.Dash.IsInstantiated 置位，
                // 内部 Instantiate() 会经 BroadcastMessage 回填 SeinAbilities.Dash。
                // set_IsInstantiated 对同值会提前 return，每帧调用没有开销。
                SeinPrefabFactory prefabs = sein.Prefabs;
                if (prefabs != null)
                    prefabs.EnsureRightPrefabsAreThereForAbilities();

                // ④ 次数与冷却（组件刚由 ③ 实例化，此处已可读到）
                SeinAbilities abilities = sein.Abilities;
                if (abilities == null) return;

                SeinDashAttack dash = abilities.Dash;
                if (dash == null) return;

                dash.ResetDashLimit();             // public 方法，就是 m_hasDashed = false
                _fLastDashTime.SetValue(dash, 0f); // 清 0.4s 冷却
            }
            catch { }
        }
    }
}
