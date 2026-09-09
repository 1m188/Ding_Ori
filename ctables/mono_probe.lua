-- OriTrainer probe: locate SaveGameController singleton and SaveGameData field offsets
local out = io.open('D:/code/Ding_Ori/ctables/mono_probe_result.txt', 'w')
local function log(s) out:write(s .. '\n') out:flush() end

local ok, err = pcall(function()
  if not mono then error('mono module not loaded') end
  log('mono functions available')
  local dc = LaunchMonoDataCollector()
  log('datacollector handle: ' .. tostring(dc))
  local enum = enumMonoDomains()
  log('domains: ' .. tostring(enum))
  for _, d in pairs(enum or {}) do
    log('  domain: ' .. tostring(d))
  end
  local asm = nil
  for _, a in pairs(enumMonoAssemblies() or {}) do
    local img = mono_getImageFromAssembly(a)
    local name = mono_image_get_name(img)
    log('assembly: ' .. name)
    if name == 'Assembly-CSharp' then asm = a end
  end
  if not asm then error('Assembly-CSharp not found') end
  local img = mono_getImageFromAssembly(asm)

  local function findClass(n)
    return mono_image_findClass(img, n, '')
  end

  -- SaveGameController: dump static fields
  local cls = findClass('SaveGameController')
  log('SaveGameController class: ' .. tostring(cls))
  if cls then
    local fields = mono_class_enumFields(cls)
    for _, f in pairs(fields or {}) do
      log(string.format('  SGC field: %s offset=%s type=%s isStatic=%s', f.name, tostring(f.offset), tostring(f.monoType), tostring(f.isStatic)))
    end
    local sfield = mono_class_findField(cls, 'Instance') or mono_class_findField(cls, 'instance') or mono_class_findField(cls, 'singleton')
    log('instance field: ' .. tostring(sfield))
    if sfield then
      local sval = mono_class_getStaticFieldAddress(cls)
      log('static field base addr: ' .. tostring(sval))
    end
  end

  -- SaveGameData: dump instance field offsets
  local cls2 = findClass('SaveGameData')
  log('SaveGameData class: ' .. tostring(cls2))
  if cls2 then
    local fields2 = mono_class_enumFields(cls2)
    for _, f in pairs(fields2 or {}) do
      log(string.format('  SGD field: %s offset=%s type=%s', f.name, tostring(f.offset), tostring(f.monoType)))
    end
  end

  -- SeinEnergy / SeinHealth classes
  for _, cn in ipairs({'SeinEnergy','SeinHealth','SeinAbilities','SeinDeathCounter','SeinCharacter'}) do
    local c = findClass(cn)
    if c then
      log(cn .. ' class found: ' .. tostring(c))
      local fs = mono_class_enumFields(c)
      for _, f in pairs(fs or {}) do
        if f.isStatic then
          log(string.format('  %s STATIC: %s offset=%s', cn, f.name, tostring(f.offset)))
        end
      end
    else
      log(cn .. ' NOT FOUND')
    end
  end
end)

if not ok then log('ERROR: ' .. tostring(err)) end
log('=== DONE ===')
out:close()
