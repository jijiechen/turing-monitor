// Virtual display creation on macOS.
//
// macOS has no IddCx equivalent, so there is no supported way for a userspace
// program to add a display. The CGVirtualDisplay classes are private API that
// lives inside the public CoreGraphics framework; they are what DisplayLink,
// BetterDisplay and DeskPad all use. Nothing here is documented by Apple, so
// this could break in any OS update.
//
// The classes are declared as protocols and looked up with NSClassFromString
// rather than being declared as interfaces: they are not exported as linkable
// symbols, so referring to them directly fails at link time. Going through the
// runtime also means a missing class degrades to a nil lookup and a clean error
// instead of a load-time crash.

#import <Foundation/Foundation.h>
#import <CoreGraphics/CoreGraphics.h>
#import <dispatch/dispatch.h>
#include <math.h>
#include <stdint.h>

@protocol TURZXVirtualDisplayMode
- (instancetype)initWithWidth:(NSUInteger)width
                       height:(NSUInteger)height
                  refreshRate:(CGFloat)refreshRate;
@end

@protocol TURZXVirtualDisplayDescriptor
@property(retain) id queue;
@property(retain) id name;
@property unsigned int maxPixelsHigh;
@property unsigned int maxPixelsWide;
@property CGSize sizeInMillimeters;
@property unsigned int serialNum;
@property unsigned int productID;
@property unsigned int vendorID;
- (void)setDispatchQueue:(id)queue;
@end

@protocol TURZXVirtualDisplaySettings
@property(retain) id modes;
@property unsigned int hiDPI;
@end

@protocol TURZXVirtualDisplay
@property(readonly) CGDirectDisplayID displayID;
- (instancetype)initWithDescriptor:(id)descriptor;
- (BOOL)applySettings:(id)settings;
@end

// The display must outlive this call or macOS tears it down.
static id gDisplay = nil;

// kLogicalDPI is the pixel density reported for the panel. macOS chooses a
// scaling mode from the ratio of pixel size to physical size, so this decides
// whether the desktop comes out 1:1 or HiDPI-halved. Around 100 DPI yields a
// true 1:1 desktop at the panel's pixel resolution, which is what a 720x1280
// side monitor wants; a realistic 5" physical size would instead be treated as
// a retina panel and give a cramped 360x640 logical area.
static const double kLogicalDPI = 100.0;

int turzx_vd_create(const char *name, int width, int height, int refreshHz, uint32_t *outID) {
	@autoreleasepool {
		if (gDisplay != nil) {
			if (outID) *outID = (uint32_t)[gDisplay displayID];
			return 0;
		}
		if (width <= 0 || height <= 0) {
			return -1;
		}

		Class descClass = NSClassFromString(@"CGVirtualDisplayDescriptor");
		Class modeClass = NSClassFromString(@"CGVirtualDisplayMode");
		Class settingsClass = NSClassFromString(@"CGVirtualDisplaySettings");
		Class displayClass = NSClassFromString(@"CGVirtualDisplay");
		if (descClass == nil || modeClass == nil || settingsClass == nil || displayClass == nil) {
			return -4;
		}

		id<TURZXVirtualDisplayDescriptor> desc = [[descClass alloc] init];
		[desc setName:[NSString stringWithUTF8String:name]];
		[desc setDispatchQueue:dispatch_get_main_queue()];
		[desc setVendorID:0x1CBE];
		[desc setProductID:0x0050];
		[desc setSerialNum:1];
		// Leave headroom so macOS can offer scaled modes without rejecting the
		// descriptor outright.
		[desc setMaxPixelsWide:(unsigned int)(width * 2)];
		[desc setMaxPixelsHigh:(unsigned int)(height * 2)];
		[desc setSizeInMillimeters:CGSizeMake(width / kLogicalDPI * 25.4,
		                                      height / kLogicalDPI * 25.4)];

		id<TURZXVirtualDisplay> display = [[displayClass alloc] initWithDescriptor:desc];
		if (display == nil) {
			return -2;
		}

		id<TURZXVirtualDisplayMode> mode = [[modeClass alloc] initWithWidth:(NSUInteger)width
		                                                            height:(NSUInteger)height
		                                                       refreshRate:(CGFloat)refreshHz];
		id<TURZXVirtualDisplaySettings> settings = [[settingsClass alloc] init];
		[settings setHiDPI:0];
		[settings setModes:@[ mode ]];
		if (![display applySettings:settings]) {
			return -3;
		}

		gDisplay = display;
		if (outID) *outID = (uint32_t)[display displayID];
		return 0;
	}
}

void turzx_vd_destroy(void) {
	@autoreleasepool {
		gDisplay = nil;
	}
}

uint32_t turzx_vd_display_id(void) {
	return gDisplay == nil ? 0 : (uint32_t)[gDisplay displayID];
}
