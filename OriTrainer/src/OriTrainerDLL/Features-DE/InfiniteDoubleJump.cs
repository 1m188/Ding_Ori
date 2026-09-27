using System;
using System.Reflection;

namespace OriTrainerDLL.Features
{
    // 无限二段跳。
    //
    // 游戏的门槛在 SeinDoubleJump.CanDoubleJump，共 5 项：
    //     enabled && !IsOnGround && m_numberOfJumpsAvailable != 0
    //             && m_remainingLockTime <= 0 && !IsInside(限制区)
    //
    // 前三项之外，这里覆盖：
    //   ① 授予基础二段跳能力 —— 没有该能力时组件不会被激活，跳跃分支根本不进入；
    //   ② 持续维持 m_numberOfJumpsAvailable —— 落地时游戏会 ResetDoubleJump() 归位，
    //      空中每跳一次也会自减，所以必须持续写；
    //   ③ 持续清 m_remainingLockTime —— 游戏会 LockForDuration() 临时上锁，
    //      只写次数仍会被锁挡住（golang 版漏了这一项）。
    //
    // ---- 为什么必须挂主线程钩子，而不能像其他功能那样用定时器 ----
    //
    // 只写 HasAbility 是**没用的**：SeinPrefabFactory.Awake() 只自动实例化 13 个组件
    // （Carry/Crouch/Fall/Jump/PushAgainstWall/Run/Idle/StandingOnEdge/Swimming/
    // SoulFlame/GrabPushPull/SpiritFlame/PickupProcessor），**DoubleJump 不在其中**。
    // 而 SeinAbilities.DoubleJump 全程序只有 SeinDoubleJump.SetReferenceToSein() 会写，
    // 后者由 SeinNestedPrefab.Instantiate() → SeinCharacter.MakeBelongToSein() →
    // BroadcastMessage("SetReferenceToSein") 回填。未实例化时它恒为 null —— 此时无论
    // 怎么写跳跃次数都毫无效果。
    //
    // 这正是"开了功能没用，捡到精灵之火（或买任意技能）之后才生效"的原因：
    // SetAbility 的最后一行无条件调用 EnsureRightPrefabsAreThereForAbilities()，
    // 它按当前 HasAbility 值补建组件，于是此前写好的标志位才终于兑现。
    // 读档（PlayerAbilities.Serialize 在 Reading 分支）同样会触发这一次补建。
    // 老存档里已有二段跳，组件早就建好，所以这个缺陷平时被掩盖着。
    //
    // 实例化的唯一入口是 SeinPrefabFactory.EnsureRightPrefabsAreThereForAbilities()，
    // 它内部走 Object.Instantiate 创建 GameObject —— Unity API，**必须在主线程调用**
    // （从定时器线程调 Unity 对象创建不安全，参见此前 Application.Quit() 崩游戏的教训）。
    // 因此本功能挂到 GameController.FixedUpdate 里调用的 UberDelegate：
    //     Game.Events.Scheduler.OnGameFixedUpdate
    // 这是游戏自己的每帧主线程回调，官方 CheatsHandler 也用同样方式挂 OnGameReset。
    //
    // 频率足够：该回调每个 FixedUpdate 触发一次，而跳跃发生在 SeinLogicCycle.FixedUpdate
    // 内，同为固定步长，最坏情况差一帧，人手按键远达不到这个速度。
    //
    // 不做官方 CheatsHandler.InfiniteDoubleJumps 那条路：它和成就闸门 DebugWasEnabled
    // 同属一套作弊系统，本功能只碰能力与跳跃字段，不进入该系统。
    public static class InfiniteDoubleJump
    {
        private const int JumpsAvailable = 999;

        private static readonly BindingFlags Private =
            BindingFlags.NonPublic | BindingFlags.Instance;

        private static FieldInfo _fJumps;    // m_numberOfJumpsAvailable
        private static FieldInfo _fLockTime; // m_remainingLockTime

        private static Action _hook; // 保留引用以便 Stop 时注销

        public static void Start()
        {
            if (_hook != null) return; // 幂等：重复 Start 不重复挂载

            _fJumps = typeof(SeinDoubleJump).GetField("m_numberOfJumpsAvailable", Private);
            _fLockTime = typeof(SeinDoubleJump).GetField("m_remainingLockTime", Private);

            // 字段名对不上就直接失败（Loader 会记进错误日志），而不是每帧静默空转
            if (_fJumps == null || _fLockTime == null)
                throw new Exception("SeinDoubleJump 的字段名与预期不符，功能无法工作");

            // Scheduler 由 GameController 持有，而 GameController.Awake 是单例守卫
            // （Instance 已存在则 Destroy 自身），所以该回调在整个进程内稳定可用。
            GameScheduler scheduler = Game.Events.Scheduler ?? throw new Exception("GameScheduler 尚未就绪（游戏未启动完成），功能无法挂载");

            _hook = OnGameFixedUpdate;
            scheduler.OnGameFixedUpdate.Add(_hook);
        }

        public static void Stop()
        {
            if (_hook == null) return;

            Game.Events.Scheduler.OnGameFixedUpdate.Remove(_hook);
            _hook = null;

            // 刻意不还原 HasAbility：游戏的 SeinNestedPrefab.IsInstantiated setter
            // 置 false 时会 Destroy() 掉能力组件，还原后再次开启无法只靠写标志位重建，
            // 会变成"关了再开就失效"。副作用是关闭后仍保留普通二段跳（一次空中跳）。
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

                // ① 能力开关（CharacterAbility.HasAbility 是普通类字段，直接写）
                PlayerAbilities playerAbilities = sein.PlayerAbilities;
                if (playerAbilities == null) return;

                if (playerAbilities.DoubleJump != null)
                    playerAbilities.DoubleJump.HasAbility = true;

                // ② 实例化二段跳组件（本功能唯一需要主线程的一步）。
                // 它按上面的开关把 SeinNestedPrefab.DoubleJump.IsInstantiated 置位，
                // 内部 Instantiate() 会经 BroadcastMessage 回填 SeinAbilities.DoubleJump。
                // set_IsInstantiated 对同值会提前 return，每帧调用没有开销。
                SeinPrefabFactory prefabs = sein.Prefabs;
                prefabs?.EnsureRightPrefabsAreThereForAbilities();

                // ③④ 跳跃次数与锁定时间（组件刚由 ② 实例化，此处已可读到）
                SeinAbilities abilities = sein.Abilities;
                if (abilities == null) return;

                SeinDoubleJump jump = abilities.DoubleJump;
                if (jump == null) return;

                _fJumps.SetValue(jump, JumpsAvailable);
                _fLockTime.SetValue(jump, 0f);
            }
            catch { }
        }
    }
}
